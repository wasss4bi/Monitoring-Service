--
-- PostgreSQL database dump
--

\restrict INxGQzMoUrAXbRXHMqG6E9QpV112eLVoOeYjNrWHUcdxlsOCeLvwLf8WeaiImDM

-- Dumped from database version 16.12
-- Dumped by pg_dump version 16.12

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

--
-- Name: public; Type: SCHEMA; Schema: -; Owner: -
--

CREATE SCHEMA public;


--
-- Name: SCHEMA public; Type: COMMENT; Schema: -; Owner: -
--

COMMENT ON SCHEMA public IS 'standard public schema';


SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: contacts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.contacts (
    id bigint NOT NULL,
    name character varying(50) NOT NULL
);


--
-- Name: contacts_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

ALTER TABLE public.contacts ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME public.contacts_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);


--
-- Name: endpoint_fetches; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.endpoint_fetches (
    fetch_id uuid DEFAULT gen_random_uuid() NOT NULL,
    endpoint_id bigint NOT NULL,
    generation bigint NOT NULL,
    scheduled_at timestamp with time zone NOT NULL,
    started_at timestamp with time zone,
    finished_at timestamp with time zone,
    status character varying(30) DEFAULT 'pending'::character varying NOT NULL,
    lease_token uuid,
    lease_until timestamp with time zone,
    response_status smallint,
    response_json jsonb,
    last_error text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    monitor_id bigint,
    CONSTRAINT endpoint_fetches_generation_check CHECK ((generation > 0)),
    CONSTRAINT endpoint_fetches_last_error_length_check CHECK (((last_error IS NULL) OR (length(last_error) <= 2048))),
    CONSTRAINT endpoint_fetches_response_status_check CHECK (((response_status IS NULL) OR ((response_status >= 100) AND (response_status <= 599)))),
    CONSTRAINT endpoint_fetches_status_check CHECK (((status)::text = ANY ((ARRAY['pending'::character varying, 'fetching'::character varying, 'succeeded'::character varying, 'failed'::character varying])::text[])))
);


--
-- Name: idempotency_keys; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.idempotency_keys (
    key character varying(255) NOT NULL,
    status character varying(20) DEFAULT 'processing'::character varying NOT NULL,
    request_hash text NOT NULL,
    response_code smallint,
    response_body jsonb,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    user_id bigint,
    ip character varying(45) DEFAULT ''::character varying NOT NULL,
    device character varying(50) DEFAULT ''::character varying NOT NULL,
    browser character varying(100) DEFAULT ''::character varying NOT NULL
);


--
-- Name: monitor_fetch_evaluations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.monitor_fetch_evaluations (
    fetch_id uuid NOT NULL,
    monitor_id bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: monitor_rules; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.monitor_rules (
    id bigint NOT NULL,
    monitor_id bigint NOT NULL,
    json_path text NOT NULL,
    comparison_operator character varying(2) NOT NULL,
    expected_value double precision NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    source jsonb,
    number_format text DEFAULT 'number'::text NOT NULL,
    description character varying(255) NOT NULL,
    CONSTRAINT monitor_rules_description_check CHECK (((length(btrim((description)::text)) >= 1) AND (length(btrim((description)::text)) <= 255))),
    CONSTRAINT monitor_rules_json_path_length_check CHECK ((length(json_path) <= 1024)),
    CONSTRAINT monitor_rules_number_format_check CHECK ((number_format = ANY (ARRAY['number'::text, 'money'::text]))),
    CONSTRAINT monitor_rules_operator_check CHECK (((comparison_operator)::text = ANY ((ARRAY['<'::character varying, '>'::character varying, '='::character varying, '<='::character varying, '>='::character varying, '!='::character varying])::text[])))
);


--
-- Name: monitor_rules_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

ALTER TABLE public.monitor_rules ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME public.monitor_rules_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);


--
-- Name: monitored_endpoints; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.monitored_endpoints (
    id bigint NOT NULL,
    canonical_url text NOT NULL,
    status character varying(30) DEFAULT 'idle'::character varying NOT NULL,
    next_check_at timestamp with time zone,
    last_attempt_at timestamp with time zone,
    last_success_at timestamp with time zone,
    last_checked_at timestamp with time zone,
    attempts_count bigint DEFAULT 0 NOT NULL,
    consecutive_errors integer DEFAULT 0 NOT NULL,
    retry_started_at timestamp with time zone,
    last_error text,
    lease_token uuid,
    lease_until timestamp with time zone,
    generation bigint DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    type character varying(4) DEFAULT 'json'::character varying NOT NULL,
    headers jsonb DEFAULT '{}'::jsonb NOT NULL,
    headers_fingerprint text DEFAULT ''::text NOT NULL,
    target_hostname_id bigint NOT NULL,
    browser_page_url text,
    json_source jsonb,
    CONSTRAINT monitored_endpoints_attempts_count_check CHECK ((attempts_count >= 0)),
    CONSTRAINT monitored_endpoints_browser_page_url_check CHECK (((browser_page_url IS NULL) OR (((type)::text = 'json'::text) AND (browser_page_url ~~ 'https://%'::text) AND (length(browser_page_url) <= 8192)))),
    CONSTRAINT monitored_endpoints_consecutive_errors_check CHECK ((consecutive_errors >= 0)),
    CONSTRAINT monitored_endpoints_generation_check CHECK ((generation >= 0)),
    CONSTRAINT monitored_endpoints_headers_object_check CHECK ((jsonb_typeof(headers) = 'object'::text)),
    CONSTRAINT monitored_endpoints_json_source_check CHECK (((json_source IS NULL) OR (((type)::text = 'json'::text) AND (browser_page_url IS NULL) AND (jsonb_typeof(json_source) = 'object'::text)))),
    CONSTRAINT monitored_endpoints_last_error_length_check CHECK (((last_error IS NULL) OR (length(last_error) <= 2048))),
    CONSTRAINT monitored_endpoints_status_check CHECK (((status)::text = ANY ((ARRAY['idle'::character varying, 'scheduled'::character varying, 'fetching'::character varying, 'retry_wait'::character varying, 'error'::character varying, 'access_denied'::character varying])::text[]))),
    CONSTRAINT monitored_endpoints_type_check CHECK (((type)::text = ANY ((ARRAY['json'::character varying, 'html'::character varying])::text[])))
);


--
-- Name: monitored_endpoints_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

ALTER TABLE public.monitored_endpoints ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME public.monitored_endpoints_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);


--
-- Name: monitors; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.monitors (
    id bigint NOT NULL,
    user_id bigint NOT NULL,
    endpoint_id bigint NOT NULL,
    status character varying(30) DEFAULT 'monitoring'::character varying NOT NULL,
    last_checked_at timestamp with time zone,
    last_error text,
    completed_at timestamp with time zone,
    stopped_at timestamp with time zone,
    deleted_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    name character varying(255) NOT NULL,
    path_type character varying(32) NOT NULL,
    rules_count_to_finish integer NOT NULL,
    headers jsonb DEFAULT '{}'::jsonb NOT NULL,
    session_mode text DEFAULT 'managed'::text NOT NULL,
    session_ciphertext text,
    session_status text DEFAULT 'missing'::text NOT NULL,
    session_generation bigint DEFAULT 0 NOT NULL,
    session_updated_at timestamp with time zone,
    refresh_lease_token uuid,
    refresh_lease_until timestamp with time zone,
    last_session_error text,
    check_status text DEFAULT 'idle'::text NOT NULL,
    next_check_at timestamp with time zone,
    attempts_count bigint DEFAULT 0 NOT NULL,
    consecutive_errors integer DEFAULT 0 NOT NULL,
    retry_started_at timestamp with time zone,
    last_attempt_at timestamp with time zone,
    last_success_at timestamp with time zone,
    profile_generation bigint DEFAULT 0 NOT NULL,
    generation bigint DEFAULT 0 NOT NULL,
    page_url text NOT NULL,
    CONSTRAINT monitors_check_status_check CHECK ((check_status = ANY (ARRAY['idle'::text, 'scheduled'::text, 'fetching'::text, 'retry_wait'::text, 'error'::text, 'access_denied'::text]))),
    CONSTRAINT monitors_last_error_length_check CHECK (((last_error IS NULL) OR (length(last_error) <= 2048))),
    CONSTRAINT monitors_name_not_blank_check CHECK (((length(btrim((name)::text)) >= 1) AND (length(btrim((name)::text)) <= 255))),
    CONSTRAINT monitors_page_url_check CHECK (((page_url ~~ 'https://%'::text) AND ((length(page_url) >= 9) AND (length(page_url) <= 8192)))),
    CONSTRAINT monitors_path_type_not_blank_check CHECK (((length(btrim((path_type)::text)) >= 1) AND (length(btrim((path_type)::text)) <= 32))),
    CONSTRAINT monitors_rules_count_to_finish_check CHECK ((rules_count_to_finish >= 1)),
    CONSTRAINT monitors_session_generation_check CHECK ((session_generation >= 0)),
    CONSTRAINT monitors_session_mode_check CHECK ((session_mode = ANY (ARRAY['managed'::text, 'provided'::text]))),
    CONSTRAINT monitors_session_status_check CHECK ((session_status = ANY (ARRAY['missing'::text, 'ready'::text, 'refreshing'::text, 'blocked'::text]))),
    CONSTRAINT monitors_status_check CHECK (((status)::text = ANY ((ARRAY['monitoring'::character varying, 'success'::character varying, 'error'::character varying, 'stopped'::character varying])::text[])))
);


--
-- Name: monitors_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

ALTER TABLE public.monitors ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME public.monitors_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);


--
-- Name: notifications; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.notifications (
    id bigint NOT NULL,
    monitor_id bigint NOT NULL,
    user_contact_id bigint,
    event_type character varying(100) NOT NULL,
    message text NOT NULL,
    status character varying(30) DEFAULT 'pending'::character varying NOT NULL,
    attempts_count integer DEFAULT 0 NOT NULL,
    last_error text,
    next_attempt_at timestamp with time zone DEFAULT now() NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    sent_at timestamp with time zone,
    idempotency_key uuid DEFAULT gen_random_uuid() NOT NULL,
    CONSTRAINT notifications_attempts_count_check CHECK ((attempts_count >= 0)),
    CONSTRAINT notifications_last_error_length_check CHECK (((last_error IS NULL) OR (length(last_error) <= 2048))),
    CONSTRAINT notifications_status_check CHECK (((status)::text = ANY ((ARRAY['pending'::character varying, 'sending'::character varying, 'sent'::character varying, 'failed'::character varying, 'skipped'::character varying])::text[])))
);


--
-- Name: notifications_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

ALTER TABLE public.notifications ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME public.notifications_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);


--
-- Name: outbox_events; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.outbox_events (
    id bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    published_at timestamp with time zone,
    lease_token uuid,
    lease_until timestamp with time zone,
    publish_attempts integer DEFAULT 0 NOT NULL,
    event_id uuid DEFAULT gen_random_uuid() NOT NULL,
    event_type character varying(100) NOT NULL,
    aggregate_type character varying(100) NOT NULL,
    aggregate_id bigint NOT NULL,
    payload jsonb NOT NULL,
    next_attempt_at timestamp with time zone NOT NULL,
    last_error text
);


--
-- Name: outbox_events_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.outbox_events_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: outbox_events_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.outbox_events_id_seq OWNED BY public.outbox_events.id;


--
-- Name: processed_messages; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.processed_messages (
    consumer_name character varying(100) NOT NULL,
    message_id uuid NOT NULL,
    processed_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: schema_migrations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.schema_migrations (
    version bigint NOT NULL,
    dirty boolean NOT NULL
);


--
-- Name: target_hostnames; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.target_hostnames (
    id bigint NOT NULL,
    hostname text NOT NULL,
    bootstrap_url text,
    limit_per_minute integer NOT NULL,
    is_active boolean DEFAULT true NOT NULL,
    session_ciphertext text,
    session_status character varying(20) DEFAULT 'missing'::character varying NOT NULL,
    session_generation bigint DEFAULT 0 NOT NULL,
    refresh_lease_token uuid,
    refresh_lease_until timestamp with time zone,
    session_updated_at timestamp with time zone,
    last_session_error text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT target_hostnames_hostname_normalized_check CHECK (((hostname = lower(hostname)) AND (hostname = btrim(hostname)) AND ((length(hostname) >= 1) AND (length(hostname) <= 253)))),
    CONSTRAINT target_hostnames_last_error_length_check CHECK (((last_session_error IS NULL) OR (length(last_session_error) <= 2048))),
    CONSTRAINT target_hostnames_limit_check CHECK (((limit_per_minute >= 1) AND (limit_per_minute <= 60000))),
    CONSTRAINT target_hostnames_session_generation_check CHECK ((session_generation >= 0)),
    CONSTRAINT target_hostnames_session_status_check CHECK (((session_status)::text = ANY ((ARRAY['missing'::character varying, 'ready'::character varying, 'refreshing'::character varying, 'blocked'::character varying])::text[])))
);


--
-- Name: target_hostnames_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

ALTER TABLE public.target_hostnames ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME public.target_hostnames_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);


--
-- Name: user_contacts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.user_contacts (
    id bigint NOT NULL,
    user_id bigint NOT NULL,
    contact_id bigint NOT NULL,
    contact character varying(255) NOT NULL,
    is_verified boolean DEFAULT false NOT NULL,
    is_active boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: user_contacts_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

ALTER TABLE public.user_contacts ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME public.user_contacts_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);


--
-- Name: users; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.users (
    id bigint NOT NULL,
    name text NOT NULL,
    email text NOT NULL,
    password text NOT NULL
);


--
-- Name: users_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.users_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: users_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.users_id_seq OWNED BY public.users.id;


--
-- Name: outbox_events id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.outbox_events ALTER COLUMN id SET DEFAULT nextval('public.outbox_events_id_seq'::regclass);


--
-- Name: users id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.users ALTER COLUMN id SET DEFAULT nextval('public.users_id_seq'::regclass);


--
-- Name: contacts contacts_name_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.contacts
    ADD CONSTRAINT contacts_name_key UNIQUE (name);


--
-- Name: contacts contacts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.contacts
    ADD CONSTRAINT contacts_pkey PRIMARY KEY (id);


--
-- Name: endpoint_fetches endpoint_fetches_monitor_generation_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.endpoint_fetches
    ADD CONSTRAINT endpoint_fetches_monitor_generation_key UNIQUE (monitor_id, generation);


--
-- Name: endpoint_fetches endpoint_fetches_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.endpoint_fetches
    ADD CONSTRAINT endpoint_fetches_pkey PRIMARY KEY (fetch_id);


--
-- Name: idempotency_keys idempotency_keys_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.idempotency_keys
    ADD CONSTRAINT idempotency_keys_pkey PRIMARY KEY (key);


--
-- Name: monitor_fetch_evaluations monitor_fetch_evaluations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.monitor_fetch_evaluations
    ADD CONSTRAINT monitor_fetch_evaluations_pkey PRIMARY KEY (fetch_id, monitor_id);


--
-- Name: monitor_rules monitor_rules_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.monitor_rules
    ADD CONSTRAINT monitor_rules_pkey PRIMARY KEY (id);


--
-- Name: monitored_endpoints monitored_endpoints_canonical_url_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.monitored_endpoints
    ADD CONSTRAINT monitored_endpoints_canonical_url_key UNIQUE (canonical_url);


--
-- Name: monitored_endpoints monitored_endpoints_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.monitored_endpoints
    ADD CONSTRAINT monitored_endpoints_pkey PRIMARY KEY (id);


--
-- Name: monitors monitors_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.monitors
    ADD CONSTRAINT monitors_pkey PRIMARY KEY (id);


--
-- Name: notifications notifications_idempotency_key_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.notifications
    ADD CONSTRAINT notifications_idempotency_key_key UNIQUE (idempotency_key);


--
-- Name: notifications notifications_monitor_event_contact_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.notifications
    ADD CONSTRAINT notifications_monitor_event_contact_key UNIQUE (monitor_id, event_type, user_contact_id);


--
-- Name: notifications notifications_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.notifications
    ADD CONSTRAINT notifications_pkey PRIMARY KEY (id);


--
-- Name: outbox_events outbox_events_event_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.outbox_events
    ADD CONSTRAINT outbox_events_event_id_key UNIQUE (event_id);


--
-- Name: outbox_events outbox_events_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.outbox_events
    ADD CONSTRAINT outbox_events_pkey PRIMARY KEY (id);


--
-- Name: processed_messages processed_messages_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.processed_messages
    ADD CONSTRAINT processed_messages_pkey PRIMARY KEY (consumer_name, message_id);


--
-- Name: schema_migrations schema_migrations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.schema_migrations
    ADD CONSTRAINT schema_migrations_pkey PRIMARY KEY (version);


--
-- Name: target_hostnames target_hostnames_hostname_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.target_hostnames
    ADD CONSTRAINT target_hostnames_hostname_unique UNIQUE (hostname);


--
-- Name: target_hostnames target_hostnames_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.target_hostnames
    ADD CONSTRAINT target_hostnames_pkey PRIMARY KEY (id);


--
-- Name: user_contacts user_contacts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_contacts
    ADD CONSTRAINT user_contacts_pkey PRIMARY KEY (id);


--
-- Name: user_contacts user_contacts_user_id_contact_id_contact_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_contacts
    ADD CONSTRAINT user_contacts_user_id_contact_id_contact_key UNIQUE (user_id, contact_id, contact);


--
-- Name: users users_email_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_email_key UNIQUE (email);


--
-- Name: users users_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_pkey PRIMARY KEY (id);


--
-- Name: endpoint_fetches_endpoint_created_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX endpoint_fetches_endpoint_created_idx ON public.endpoint_fetches USING btree (endpoint_id, created_at DESC);


--
-- Name: endpoint_fetches_lease_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX endpoint_fetches_lease_idx ON public.endpoint_fetches USING btree (lease_until, fetch_id) WHERE ((status)::text = 'fetching'::text);


--
-- Name: endpoint_fetches_legacy_generation_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX endpoint_fetches_legacy_generation_idx ON public.endpoint_fetches USING btree (endpoint_id, generation) WHERE (monitor_id IS NULL);


--
-- Name: endpoint_fetches_monitor_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX endpoint_fetches_monitor_idx ON public.endpoint_fetches USING btree (monitor_id, created_at);


--
-- Name: endpoint_fetches_retention_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX endpoint_fetches_retention_idx ON public.endpoint_fetches USING btree (created_at);


--
-- Name: idempotency_keys_key_expires_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idempotency_keys_key_expires_at_idx ON public.idempotency_keys USING btree (key, expires_at);


--
-- Name: monitor_rules_monitor_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX monitor_rules_monitor_id_idx ON public.monitor_rules USING btree (monitor_id);


--
-- Name: monitored_endpoints_due_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX monitored_endpoints_due_idx ON public.monitored_endpoints USING btree (next_check_at, id) WHERE ((next_check_at IS NOT NULL) AND ((status)::text = ANY ((ARRAY['scheduled'::character varying, 'retry_wait'::character varying])::text[])));


--
-- Name: monitored_endpoints_schedule_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX monitored_endpoints_schedule_idx ON public.monitored_endpoints USING btree (next_check_at, id) WHERE ((status)::text = ANY ((ARRAY['idle'::character varying, 'scheduled'::character varying, 'retry_wait'::character varying])::text[]));


--
-- Name: monitored_endpoints_target_hostname_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX monitored_endpoints_target_hostname_id_idx ON public.monitored_endpoints USING btree (target_hostname_id);


--
-- Name: monitors_due_check_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX monitors_due_check_idx ON public.monitors USING btree (next_check_at, id) WHERE (((status)::text = 'monitoring'::text) AND (deleted_at IS NULL) AND (check_status = ANY (ARRAY['idle'::text, 'retry_wait'::text])));


--
-- Name: monitors_endpoint_active_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX monitors_endpoint_active_idx ON public.monitors USING btree (endpoint_id, status) WHERE ((deleted_at IS NULL) AND ((status)::text = 'monitoring'::text));


--
-- Name: monitors_user_created_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX monitors_user_created_id_idx ON public.monitors USING btree (user_id, created_at DESC, id DESC) WHERE (deleted_at IS NULL);


--
-- Name: notifications_contact_pending_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX notifications_contact_pending_idx ON public.notifications USING btree (user_contact_id, next_attempt_at, id) WHERE ((status)::text = ANY ((ARRAY['pending'::character varying, 'sending'::character varying])::text[]));


--
-- Name: notifications_monitor_created_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX notifications_monitor_created_idx ON public.notifications USING btree (monitor_id, created_at DESC, id DESC);


--
-- Name: notifications_pending_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX notifications_pending_idx ON public.notifications USING btree (next_attempt_at, id) WHERE ((status)::text = 'pending'::text);


--
-- Name: notifications_retention_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX notifications_retention_idx ON public.notifications USING btree (created_at);


--
-- Name: outbox_events_pending_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX outbox_events_pending_idx ON public.outbox_events USING btree (next_attempt_at, id) WHERE (published_at IS NULL);


--
-- Name: processed_messages_processed_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX processed_messages_processed_at_idx ON public.processed_messages USING btree (processed_at);


--
-- Name: target_hostnames_refresh_lease_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX target_hostnames_refresh_lease_idx ON public.target_hostnames USING btree (refresh_lease_until) WHERE ((session_status)::text = 'refreshing'::text);


--
-- Name: user_contacts_active_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX user_contacts_active_idx ON public.user_contacts USING btree (user_id, is_active);


--
-- Name: user_contacts_user_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX user_contacts_user_id_idx ON public.user_contacts USING btree (user_id);


--
-- Name: endpoint_fetches endpoint_fetches_endpoint_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.endpoint_fetches
    ADD CONSTRAINT endpoint_fetches_endpoint_id_fkey FOREIGN KEY (endpoint_id) REFERENCES public.monitored_endpoints(id) ON DELETE CASCADE;


--
-- Name: endpoint_fetches endpoint_fetches_monitor_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.endpoint_fetches
    ADD CONSTRAINT endpoint_fetches_monitor_id_fkey FOREIGN KEY (monitor_id) REFERENCES public.monitors(id) ON DELETE CASCADE;


--
-- Name: idempotency_keys idempotency_keys_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.idempotency_keys
    ADD CONSTRAINT idempotency_keys_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: monitor_fetch_evaluations monitor_fetch_evaluations_fetch_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.monitor_fetch_evaluations
    ADD CONSTRAINT monitor_fetch_evaluations_fetch_id_fkey FOREIGN KEY (fetch_id) REFERENCES public.endpoint_fetches(fetch_id) ON DELETE CASCADE;


--
-- Name: monitor_fetch_evaluations monitor_fetch_evaluations_monitor_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.monitor_fetch_evaluations
    ADD CONSTRAINT monitor_fetch_evaluations_monitor_id_fkey FOREIGN KEY (monitor_id) REFERENCES public.monitors(id) ON DELETE CASCADE;


--
-- Name: monitor_rules monitor_rules_monitor_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.monitor_rules
    ADD CONSTRAINT monitor_rules_monitor_id_fkey FOREIGN KEY (monitor_id) REFERENCES public.monitors(id) ON DELETE CASCADE;


--
-- Name: monitored_endpoints monitored_endpoints_target_hostname_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.monitored_endpoints
    ADD CONSTRAINT monitored_endpoints_target_hostname_id_fkey FOREIGN KEY (target_hostname_id) REFERENCES public.target_hostnames(id) ON DELETE RESTRICT;


--
-- Name: monitors monitors_endpoint_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.monitors
    ADD CONSTRAINT monitors_endpoint_id_fkey FOREIGN KEY (endpoint_id) REFERENCES public.monitored_endpoints(id) ON DELETE CASCADE;


--
-- Name: monitors monitors_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.monitors
    ADD CONSTRAINT monitors_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: notifications notifications_monitor_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.notifications
    ADD CONSTRAINT notifications_monitor_id_fkey FOREIGN KEY (monitor_id) REFERENCES public.monitors(id) ON DELETE CASCADE;


--
-- Name: notifications notifications_user_contact_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.notifications
    ADD CONSTRAINT notifications_user_contact_id_fkey FOREIGN KEY (user_contact_id) REFERENCES public.user_contacts(id) ON DELETE SET NULL;


--
-- Name: user_contacts user_contacts_contact_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_contacts
    ADD CONSTRAINT user_contacts_contact_id_fkey FOREIGN KEY (contact_id) REFERENCES public.contacts(id) ON DELETE CASCADE;


--
-- Name: user_contacts user_contacts_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_contacts
    ADD CONSTRAINT user_contacts_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- PostgreSQL database dump complete
--

\unrestrict INxGQzMoUrAXbRXHMqG6E9QpV112eLVoOeYjNrWHUcdxlsOCeLvwLf8WeaiImDM

