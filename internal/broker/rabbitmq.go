package broker

import (
	"context"
	"fmt"
	amqp "github.com/rabbitmq/amqp091-go"
	"log/slog"
	"net"
	"sync"
	"time"
)

const (
	ExchangeName             = "monitoring_service.events"
	DeadLetterExchange       = "monitoring_service.dead-letter"
	LegacyEndpointFetchQueue = "monitoring_service.endpoint-fetch.v2"
	EndpointFetchJSONQueue   = "monitoring_service.endpoint-fetch-json.v2"
	EndpointFetchHTMLQueue   = "monitoring_service.endpoint-fetch-html.v2"
	NotificationQueue        = "monitoring_service.notification.v2"
)

type RabbitMQ struct {
	url       string
	mu        sync.Mutex
	conn      *amqp.Connection
	consumers map[string]*amqp.Channel
}

func New(url string) *RabbitMQ { return &RabbitMQ{url: url, consumers: map[string]*amqp.Channel{}} }
func (r *RabbitMQ) Connect(ctx context.Context) error {
	for delay := time.Second; ; {
		r.mu.Lock()
		c := r.conn
		r.mu.Unlock()
		if c != nil && !c.IsClosed() {
			return nil
		}
		d := net.Dialer{}
		conn, err := amqp.DialConfig(r.url, amqp.Config{Dial: func(n, a string) (net.Conn, error) { return d.DialContext(ctx, n, a) }})
		if err == nil {
			r.mu.Lock()
			r.conn = conn
			r.mu.Unlock()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
		if delay < 30*time.Second {
			delay *= 2
			if delay > 30*time.Second {
				delay = 30 * time.Second
			}
		}
	}
}
func (r *RabbitMQ) Channel(ctx context.Context) (*amqp.Channel, error) {
	if err := r.Connect(ctx); err != nil {
		return nil, err
	}
	r.mu.Lock()
	c := r.conn
	r.mu.Unlock()
	ch, err := c.Channel()
	if err != nil {
		return nil, fmt.Errorf("open RabbitMQ channel: %w", err)
	}
	return ch, nil
}
func (r *RabbitMQ) ExchangeDeclare(ctx context.Context, name, kind string) error {
	ch, e := r.Channel(ctx)
	if e != nil {
		return e
	}
	defer func() {
		if err := ch.Close(); err != nil {
			slog.Error("close RabbitMQ channel", "error", err)
		}
	}()
	return ch.ExchangeDeclare(name, kind, true, false, false, false, nil)
}
func (r *RabbitMQ) QueueDeclare(ctx context.Context, name string, args amqp.Table) error {
	ch, e := r.Channel(ctx)
	if e != nil {
		return e
	}
	defer func() {
		if err := ch.Close(); err != nil {
			slog.Error("close RabbitMQ channel", "error", err)
		}
	}()
	_, e = ch.QueueDeclare(name, true, false, false, false, args)
	return e
}
func (r *RabbitMQ) QueueBind(ctx context.Context, queue, key, exchange string) error {
	ch, e := r.Channel(ctx)
	if e != nil {
		return e
	}
	defer func() {
		if err := ch.Close(); err != nil {
			slog.Error("close RabbitMQ channel", "error", err)
		}
	}()
	return ch.QueueBind(queue, key, exchange, false, nil)
}
func (r *RabbitMQ) DeclareTopology(ctx context.Context) error {
	if err := r.ExchangeDeclare(ctx, ExchangeName, "direct"); err != nil {
		return err
	}
	if err := r.ExchangeDeclare(ctx, DeadLetterExchange, "direct"); err != nil {
		return err
	}
	for _, b := range []struct{ q, k string }{
		{LegacyEndpointFetchQueue, "endpoint.fetch.v2"},
		{EndpointFetchJSONQueue, "endpoint.fetch.json.v2"},
		{EndpointFetchHTMLQueue, "endpoint.fetch.html.v2"},
		{NotificationQueue, "notification.deliver.v2"},
	} {
		dlq := b.q + ".dlq"
		if err := r.QueueDeclare(ctx, dlq, nil); err != nil {
			return err
		}
		if err := r.QueueBind(ctx, dlq, dlq, DeadLetterExchange); err != nil {
			return err
		}
		if err := r.QueueDeclare(ctx, b.q, amqp.Table{"x-dead-letter-exchange": DeadLetterExchange, "x-dead-letter-routing-key": dlq}); err != nil {
			return err
		}
		if err := r.QueueBind(ctx, b.q, b.k, ExchangeName); err != nil {
			return err
		}
	}
	if err := r.QueueBind(ctx, NotificationQueue, "contact.verification.deliver", ExchangeName); err != nil {
		return err
	}
	if err := r.QueueBind(ctx, NotificationQueue, "operational.notification.deliver.v2", ExchangeName); err != nil {
		return err
	}
	return nil
}
func (r *RabbitMQ) Publish(ctx context.Context, key string, body []byte, messageID string) error {
	ch, e := r.Channel(ctx)
	if e != nil {
		return e
	}
	defer func() {
		if err := ch.Close(); err != nil {
			slog.ErrorContext(ctx, "close RabbitMQ publisher channel", "error", err)
		}
	}()
	if e = ch.Confirm(false); e != nil {
		return e
	}
	confirms := ch.NotifyPublish(make(chan amqp.Confirmation, 1))
	returns := ch.NotifyReturn(make(chan amqp.Return, 1))
	if e = ch.PublishWithContext(ctx, ExchangeName, versionedRoutingKey(key), true, false, amqp.Publishing{ContentType: "application/json", DeliveryMode: amqp.Persistent, MessageId: messageID, Timestamp: time.Now().UTC(), Body: body}); e != nil {
		return e
	}
	select {
	case c := <-confirms:
		if !c.Ack {
			return fmt.Errorf("RabbitMQ rejected publication")
		}
		select {
		case <-returns:
			return fmt.Errorf("RabbitMQ publication was not routed")
		default:
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func versionedRoutingKey(key string) string {
	switch key {
	case "endpoint.fetch", "endpoint.fetch.json", "endpoint.fetch.html", "notification.deliver", "operational.notification.deliver":
		return key + ".v2"
	default:
		return key
	}
}

func (r *RabbitMQ) QueueMessageCount(ctx context.Context, name string) (int, error) {
	ch, err := r.Channel(ctx)
	if err != nil {
		return 0, err
	}
	defer func() {
		if err := ch.Close(); err != nil {
			slog.Error("close RabbitMQ channel", "error", err)
		}
	}()
	queue, err := ch.QueueDeclarePassive(name, false, false, false, false, nil)
	if err != nil {
		return 0, err
	}
	return queue.Messages, nil
}
func (r *RabbitMQ) Consume(ctx context.Context, queue, tag string, prefetch int) (<-chan amqp.Delivery, error) {
	ch, e := r.Channel(ctx)
	if e != nil {
		return nil, e
	}
	if e = ch.Qos(prefetch, 0, false); e != nil {
		if err := ch.Close(); err != nil {
			slog.Error("close RabbitMQ consumer channel", "error", err)
		}
		return nil, e
	}
	d, e := ch.ConsumeWithContext(ctx, queue, tag, false, false, false, false, nil)
	if e != nil {
		if err := ch.Close(); err != nil {
			slog.Error("close RabbitMQ consumer channel", "error", err)
		}
		return nil, e
	}
	r.mu.Lock()
	r.consumers[tag] = ch
	r.mu.Unlock()
	return d, nil
}
func (r *RabbitMQ) Cancel(tag string) error {
	r.mu.Lock()
	ch := r.consumers[tag]
	delete(r.consumers, tag)
	r.mu.Unlock()
	if ch == nil {
		return nil
	}
	defer func() {
		if err := ch.Close(); err != nil {
			slog.Error("close RabbitMQ channel", "error", err)
		}
	}()
	return ch.Cancel(tag, false)
}
func (r *RabbitMQ) Ack(d amqp.Delivery) error                { return d.Ack(false) }
func (r *RabbitMQ) Nack(d amqp.Delivery, requeue bool) error { return d.Nack(false, requeue) }
func (r *RabbitMQ) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, ch := range r.consumers {
		_ = ch.Close()
	}
	r.consumers = map[string]*amqp.Channel{}
	if r.conn != nil {
		return r.conn.Close()
	}
	return nil
}
