package broker

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
)

func TestRabbitMQPublishAndConsume(t *testing.T) {
	url := strings.TrimSpace(os.Getenv("RABBITMQ_URL"))
	if url == "" {
		t.Skip("set RABBITMQ_URL to run RabbitMQ integration test")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	b := New(url)
	defer b.Close()
	if err := b.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	suffix := uuid.NewString()
	exchange := "test." + suffix
	queue := "test." + suffix
	key := "test"
	if err := b.ExchangeDeclare(ctx, exchange, "direct"); err != nil {
		t.Fatal(err)
	}
	if err := b.QueueDeclare(ctx, queue, nil); err != nil {
		t.Fatal(err)
	}
	if err := b.QueueBind(ctx, queue, key, exchange); err != nil {
		t.Fatal(err)
	}
	ch, err := b.Channel(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer ch.Close()
	if err = ch.Confirm(false); err != nil {
		t.Fatal(err)
	}
	confirms := ch.NotifyPublish(make(chan amqp.Confirmation, 1))
	if err = ch.PublishWithContext(ctx, exchange, key, false, false, amqp.Publishing{DeliveryMode: amqp.Persistent, Body: []byte("ok")}); err != nil {
		t.Fatal(err)
	}
	if confirmation := <-confirms; !confirmation.Ack {
		t.Fatal("broker rejected publication")
	}
	tag := "test-" + suffix
	deliveries, err := b.Consume(ctx, queue, tag, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Cancel(tag)
	select {
	case d := <-deliveries:
		if string(d.Body) != "ok" {
			t.Fatalf("body=%q", d.Body)
		}
		if err := b.Ack(d); err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if _, err := ch.QueueDelete(queue, false, false, false); err != nil {
		t.Fatal(err)
	}
	if err := ch.ExchangeDelete(exchange, false, false); err != nil {
		t.Fatal(err)
	}
}

func TestVersionedRoutingKey(t *testing.T) {
	for input, want := range map[string]string{
		"endpoint.fetch":                   "endpoint.fetch.v2",
		"endpoint.fetch.json":              "endpoint.fetch.json.v2",
		"endpoint.fetch.html":              "endpoint.fetch.html.v2",
		"notification.deliver":             "notification.deliver.v2",
		"operational.notification.deliver": "operational.notification.deliver.v2",
		"other":                            "other",
	} {
		if got := versionedRoutingKey(input); got != want {
			t.Errorf("versionedRoutingKey(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestRabbitMQMandatoryRoutingAndOperationalNotifications(t *testing.T) {
	if os.Getenv("RABBITMQ_ISOLATED_TESTS") != "1" {
		t.Skip("set RABBITMQ_ISOLATED_TESTS=1 with an isolated broker; this test consumes the notification queue")
	}
	url := strings.TrimSpace(os.Getenv("RABBITMQ_URL"))
	if url == "" {
		t.Skip("set RABBITMQ_URL to run RabbitMQ integration test")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	b := New(url)
	defer b.Close()
	if err := b.DeclareTopology(ctx); err != nil {
		t.Fatal(err)
	}
	ch, err := b.Channel(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer ch.Close()
	// This queue has a unique routing key so concurrent test runs stay isolated.
	queue, err := ch.QueueDeclare("", false, true, true, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	key := "test." + uuid.NewString()
	if err := ch.QueueBind(queue.Name, key, ExchangeName, false, nil); err != nil {
		t.Fatal(err)
	}
	if err := b.Publish(ctx, key, []byte("ok"), key); err != nil {
		t.Fatal(err)
	}
	delivery, ok, err := ch.Get(queue.Name, true)
	if err != nil || !ok || string(delivery.Body) != "ok" || delivery.MessageId != key {
		t.Fatalf("confirmed publication was not delivered: ok=%v err=%v", ok, err)
	}
	if err := b.Publish(ctx, "unbound."+uuid.NewString(), []byte("discard"), ""); err == nil {
		t.Fatal("mandatory publication silently accepted an unroutable message")
	}
	if err := b.Publish(ctx, "operational.notification.deliver", []byte("{}"), key); err != nil {
		t.Fatal(err)
	}
	delivery, ok, err = ch.Get(NotificationQueue, true)
	if err != nil || !ok || delivery.RoutingKey != "operational.notification.deliver.v2" {
		t.Fatalf("operational notification did not reach notification queue: ok=%v err=%v", ok, err)
	}
}
