--
-- PostgreSQL database dump
--


-- Dumped from database version 16.12
-- Dumped by pg_dump version 16.12

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: contact_verifications; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE contact_verifications (
    contact_id bigint NOT NULL,
    challenge_id uuid NOT NULL,
    contact_value text NOT NULL,
    code_hash bytea NOT NULL,
    attempts integer DEFAULT 0 NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    requested_at timestamp with time zone NOT NULL,
    sent_at timestamp with time zone,
    lease_token uuid,
    lease_until timestamp with time zone,
    CONSTRAINT contact_verifications_attempts_check CHECK (((attempts >= 0) AND (attempts <= 5)))
);


--
-- Name: contacts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE contacts (
    id bigint NOT NULL,
    name character varying(50) NOT NULL
);


--
-- Name: contacts_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

ALTER TABLE contacts ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME contacts_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);


--
-- Name: endpoint_fetches; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE endpoint_fetches (
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
    fetch_metadata jsonb,
    defer_reason character varying(64),
    next_dispatch_at timestamp with time zone,
    available_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT endpoint_fetches_generation_check CHECK ((generation > 0)),
    CONSTRAINT endpoint_fetches_last_error_length_check CHECK (((last_error IS NULL) OR (length(last_error) <= 2048))),
    CONSTRAINT endpoint_fetches_response_status_check CHECK (((response_status IS NULL) OR ((response_status >= 100) AND (response_status <= 599)))),
    CONSTRAINT endpoint_fetches_status_check CHECK (((status)::text = ANY ((ARRAY['pending'::character varying, 'fetching'::character varying, 'succeeded'::character varying, 'failed'::character varying])::text[])))
);


--
-- Name: idempotency_keys; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE idempotency_keys (
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
    browser character varying(100) DEFAULT ''::character varying NOT NULL,
    monitor_id bigint
);


--
-- Name: monitor_fetch_evaluations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE monitor_fetch_evaluations (
    fetch_id uuid NOT NULL,
    monitor_id bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: monitor_rules; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE monitor_rules (
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

ALTER TABLE monitor_rules ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME monitor_rules_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);


--
-- Name: monitored_endpoints; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE monitored_endpoints (
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

ALTER TABLE monitored_endpoints ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME monitored_endpoints_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);


--
-- Name: monitors; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE monitors (
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

ALTER TABLE monitors ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME monitors_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);


--
-- Name: notifications; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE notifications (
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
    lease_token uuid,
    CONSTRAINT notifications_attempts_count_check CHECK ((attempts_count >= 0)),
    CONSTRAINT notifications_last_error_length_check CHECK (((last_error IS NULL) OR (length(last_error) <= 2048))),
    CONSTRAINT notifications_status_check CHECK (((status)::text = ANY ((ARRAY['pending'::character varying, 'sending'::character varying, 'sent'::character varying, 'failed'::character varying, 'skipped'::character varying])::text[])))
);


--
-- Name: notifications_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

ALTER TABLE notifications ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME notifications_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);


--
-- Name: operational_notifications; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE operational_notifications (
    id bigint NOT NULL,
    dependency_name character varying(32) NOT NULL,
    episode_id uuid NOT NULL,
    transition character varying(8) NOT NULL,
    recipient text NOT NULL,
    message text NOT NULL,
    status character varying(16) DEFAULT 'pending'::character varying NOT NULL,
    next_attempt_at timestamp with time zone DEFAULT now() NOT NULL,
    attempts integer DEFAULT 0 NOT NULL,
    lease_token uuid,
    lease_until timestamp with time zone,
    sent_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT operational_notifications_message_check CHECK ((length(message) <= 2048)),
    CONSTRAINT operational_notifications_status_check CHECK (((status)::text = ANY ((ARRAY['pending'::character varying, 'sending'::character varying, 'sent'::character varying])::text[]))),
    CONSTRAINT operational_notifications_transition_check CHECK (((transition)::text = ANY ((ARRAY['down'::character varying, 'up'::character varying])::text[])))
);


--
-- Name: operational_notifications_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE operational_notifications_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: operational_notifications_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE operational_notifications_id_seq OWNED BY operational_notifications.id;


--
-- Name: outbox_events; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE outbox_events (
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

CREATE SEQUENCE outbox_events_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: outbox_events_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE outbox_events_id_seq OWNED BY outbox_events.id;


--
-- Name: processed_messages; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE processed_messages (
    consumer_name character varying(100) NOT NULL,
    message_id uuid NOT NULL,
    processed_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: scheduler_capacity_locks; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE scheduler_capacity_locks (
    name text NOT NULL,
    CONSTRAINT scheduler_capacity_locks_name_check CHECK ((name = ANY (ARRAY['direct'::text, 'browser'::text])))
);


--
-- Name: service_dependencies; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE service_dependencies (
    name character varying(32) NOT NULL,
    status character varying(16) DEFAULT 'unknown'::character varying NOT NULL,
    failures integer DEFAULT 0 NOT NULL,
    successes integer DEFAULT 0 NOT NULL,
    episode_id uuid,
    transition_at timestamp with time zone DEFAULT now() NOT NULL,
    last_probe_at timestamp with time zone,
    lease_token uuid,
    lease_until timestamp with time zone,
    CONSTRAINT service_dependencies_status_check CHECK (((status)::text = ANY ((ARRAY['unknown'::character varying, 'up'::character varying, 'down'::character varying])::text[])))
);


--
-- Name: target_hostnames; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE target_hostnames (
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
    bootstrap_rules jsonb DEFAULT '[]'::jsonb NOT NULL,
    CONSTRAINT target_bootstrap_rules_array CHECK (((jsonb_typeof(bootstrap_rules) = 'array'::text) AND (jsonb_array_length(bootstrap_rules) <= 32))),
    CONSTRAINT target_hostnames_hostname_normalized_check CHECK (((hostname = lower(hostname)) AND (hostname = btrim(hostname)) AND ((length(hostname) >= 1) AND (length(hostname) <= 253)))),
    CONSTRAINT target_hostnames_last_error_length_check CHECK (((last_session_error IS NULL) OR (length(last_session_error) <= 2048))),
    CONSTRAINT target_hostnames_limit_check CHECK (((limit_per_minute >= 1) AND (limit_per_minute <= 60000))),
    CONSTRAINT target_hostnames_session_generation_check CHECK ((session_generation >= 0)),
    CONSTRAINT target_hostnames_session_status_check CHECK (((session_status)::text = ANY ((ARRAY['missing'::character varying, 'ready'::character varying, 'refreshing'::character varying, 'blocked'::character varying])::text[])))
);


--
-- Name: target_hostnames_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

ALTER TABLE target_hostnames ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME target_hostnames_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);


--
-- Name: user_contacts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE user_contacts (
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

ALTER TABLE user_contacts ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME user_contacts_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);


--
-- Name: users; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE users (
    id bigint NOT NULL,
    name text NOT NULL,
    email text NOT NULL,
    password text NOT NULL,
    role text DEFAULT 'user'::text NOT NULL,
    auth_version bigint DEFAULT 1 NOT NULL,
    CONSTRAINT users_auth_version_check CHECK ((auth_version > 0)),
    CONSTRAINT users_role_check CHECK ((role = ANY (ARRAY['user'::text, 'admin'::text])))
);


--
-- Name: users_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE users_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: users_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE users_id_seq OWNED BY users.id;


--
-- Name: operational_notifications id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY operational_notifications ALTER COLUMN id SET DEFAULT nextval('operational_notifications_id_seq'::regclass);


--
-- Name: outbox_events id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY outbox_events ALTER COLUMN id SET DEFAULT nextval('outbox_events_id_seq'::regclass);


--
-- Name: users id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY users ALTER COLUMN id SET DEFAULT nextval('users_id_seq'::regclass);


--
-- Name: contact_verifications contact_verifications_challenge_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY contact_verifications
    ADD CONSTRAINT contact_verifications_challenge_id_key UNIQUE (challenge_id);


--
-- Name: contact_verifications contact_verifications_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY contact_verifications
    ADD CONSTRAINT contact_verifications_pkey PRIMARY KEY (contact_id);


--
-- Name: contacts contacts_name_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY contacts
    ADD CONSTRAINT contacts_name_key UNIQUE (name);


--
-- Name: contacts contacts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY contacts
    ADD CONSTRAINT contacts_pkey PRIMARY KEY (id);


--
-- Name: endpoint_fetches endpoint_fetches_monitor_generation_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY endpoint_fetches
    ADD CONSTRAINT endpoint_fetches_monitor_generation_key UNIQUE (monitor_id, generation);


--
-- Name: endpoint_fetches endpoint_fetches_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY endpoint_fetches
    ADD CONSTRAINT endpoint_fetches_pkey PRIMARY KEY (fetch_id);


--
-- Name: idempotency_keys idempotency_keys_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY idempotency_keys
    ADD CONSTRAINT idempotency_keys_pkey PRIMARY KEY (key);


--
-- Name: monitor_fetch_evaluations monitor_fetch_evaluations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY monitor_fetch_evaluations
    ADD CONSTRAINT monitor_fetch_evaluations_pkey PRIMARY KEY (fetch_id, monitor_id);


--
-- Name: monitor_rules monitor_rules_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY monitor_rules
    ADD CONSTRAINT monitor_rules_pkey PRIMARY KEY (id);


--
-- Name: monitored_endpoints monitored_endpoints_canonical_url_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY monitored_endpoints
    ADD CONSTRAINT monitored_endpoints_canonical_url_key UNIQUE (canonical_url);


--
-- Name: monitored_endpoints monitored_endpoints_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY monitored_endpoints
    ADD CONSTRAINT monitored_endpoints_pkey PRIMARY KEY (id);


--
-- Name: monitors monitors_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY monitors
    ADD CONSTRAINT monitors_pkey PRIMARY KEY (id);


--
-- Name: notifications notifications_idempotency_key_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY notifications
    ADD CONSTRAINT notifications_idempotency_key_key UNIQUE (idempotency_key);


--
-- Name: notifications notifications_monitor_event_contact_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY notifications
    ADD CONSTRAINT notifications_monitor_event_contact_key UNIQUE (monitor_id, event_type, user_contact_id);


--
-- Name: notifications notifications_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY notifications
    ADD CONSTRAINT notifications_pkey PRIMARY KEY (id);


--
-- Name: operational_notifications operational_notifications_dependency_name_episode_id_transi_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY operational_notifications
    ADD CONSTRAINT operational_notifications_dependency_name_episode_id_transi_key UNIQUE (dependency_name, episode_id, transition, recipient);


--
-- Name: operational_notifications operational_notifications_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY operational_notifications
    ADD CONSTRAINT operational_notifications_pkey PRIMARY KEY (id);


--
-- Name: outbox_events outbox_events_event_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY outbox_events
    ADD CONSTRAINT outbox_events_event_id_key UNIQUE (event_id);


--
-- Name: outbox_events outbox_events_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY outbox_events
    ADD CONSTRAINT outbox_events_pkey PRIMARY KEY (id);


--
-- Name: processed_messages processed_messages_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY processed_messages
    ADD CONSTRAINT processed_messages_pkey PRIMARY KEY (consumer_name, message_id);


--
-- Name: scheduler_capacity_locks scheduler_capacity_locks_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY scheduler_capacity_locks
    ADD CONSTRAINT scheduler_capacity_locks_pkey PRIMARY KEY (name);


--
-- Name: service_dependencies service_dependencies_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY service_dependencies
    ADD CONSTRAINT service_dependencies_pkey PRIMARY KEY (name);


--
-- Name: target_hostnames target_hostnames_hostname_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY target_hostnames
    ADD CONSTRAINT target_hostnames_hostname_unique UNIQUE (hostname);


--
-- Name: target_hostnames target_hostnames_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY target_hostnames
    ADD CONSTRAINT target_hostnames_pkey PRIMARY KEY (id);


--
-- Name: user_contacts user_contacts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY user_contacts
    ADD CONSTRAINT user_contacts_pkey PRIMARY KEY (id);


--
-- Name: user_contacts user_contacts_user_id_contact_id_contact_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY user_contacts
    ADD CONSTRAINT user_contacts_user_id_contact_id_contact_key UNIQUE (user_id, contact_id, contact);


--
-- Name: users users_email_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY users
    ADD CONSTRAINT users_email_key UNIQUE (email);


--
-- Name: users users_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY users
    ADD CONSTRAINT users_pkey PRIMARY KEY (id);


--
-- Name: endpoint_fetches_active_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX endpoint_fetches_active_idx ON endpoint_fetches USING btree (monitor_id) WHERE ((status)::text = ANY ((ARRAY['pending'::character varying, 'fetching'::character varying])::text[]));


--
-- Name: endpoint_fetches_endpoint_created_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX endpoint_fetches_endpoint_created_idx ON endpoint_fetches USING btree (endpoint_id, created_at DESC);


--
-- Name: endpoint_fetches_lease_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX endpoint_fetches_lease_idx ON endpoint_fetches USING btree (lease_until, fetch_id) WHERE ((status)::text = 'fetching'::text);


--
-- Name: endpoint_fetches_legacy_generation_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX endpoint_fetches_legacy_generation_idx ON endpoint_fetches USING btree (endpoint_id, generation) WHERE (monitor_id IS NULL);


--
-- Name: endpoint_fetches_monitor_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX endpoint_fetches_monitor_idx ON endpoint_fetches USING btree (monitor_id, created_at);


--
-- Name: endpoint_fetches_pending_dispatch_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX endpoint_fetches_pending_dispatch_idx ON endpoint_fetches USING btree (next_dispatch_at, fetch_id) WHERE ((status)::text = 'pending'::text);


--
-- Name: endpoint_fetches_retention_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX endpoint_fetches_retention_idx ON endpoint_fetches USING btree (created_at);


--
-- Name: idempotency_keys_key_expires_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idempotency_keys_key_expires_at_idx ON idempotency_keys USING btree (key, expires_at);


--
-- Name: monitor_rules_monitor_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX monitor_rules_monitor_id_idx ON monitor_rules USING btree (monitor_id);


--
-- Name: monitored_endpoints_due_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX monitored_endpoints_due_idx ON monitored_endpoints USING btree (next_check_at, id) WHERE ((next_check_at IS NOT NULL) AND ((status)::text = ANY ((ARRAY['scheduled'::character varying, 'retry_wait'::character varying])::text[])));


--
-- Name: monitored_endpoints_schedule_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX monitored_endpoints_schedule_idx ON monitored_endpoints USING btree (next_check_at, id) WHERE ((status)::text = ANY ((ARRAY['idle'::character varying, 'scheduled'::character varying, 'retry_wait'::character varying])::text[]));


--
-- Name: monitored_endpoints_target_hostname_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX monitored_endpoints_target_hostname_id_idx ON monitored_endpoints USING btree (target_hostname_id);


--
-- Name: monitors_due_check_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX monitors_due_check_idx ON monitors USING btree (next_check_at, id) WHERE (((status)::text = 'monitoring'::text) AND (deleted_at IS NULL) AND (check_status = ANY (ARRAY['idle'::text, 'retry_wait'::text])));


--
-- Name: monitors_endpoint_active_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX monitors_endpoint_active_idx ON monitors USING btree (endpoint_id, status) WHERE ((deleted_at IS NULL) AND ((status)::text = 'monitoring'::text));


--
-- Name: monitors_user_created_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX monitors_user_created_id_idx ON monitors USING btree (user_id, created_at DESC, id DESC) WHERE (deleted_at IS NULL);


--
-- Name: notifications_contact_pending_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX notifications_contact_pending_idx ON notifications USING btree (user_contact_id, next_attempt_at, id) WHERE ((status)::text = ANY ((ARRAY['pending'::character varying, 'sending'::character varying])::text[]));


--
-- Name: notifications_monitor_created_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX notifications_monitor_created_idx ON notifications USING btree (monitor_id, created_at DESC, id DESC);


--
-- Name: notifications_pending_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX notifications_pending_idx ON notifications USING btree (next_attempt_at, id) WHERE ((status)::text = 'pending'::text);


--
-- Name: notifications_retention_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX notifications_retention_idx ON notifications USING btree (created_at);


--
-- Name: operational_notifications_due_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX operational_notifications_due_idx ON operational_notifications USING btree (next_attempt_at, id) WHERE ((status)::text <> 'sent'::text);


--
-- Name: outbox_events_pending_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX outbox_events_pending_idx ON outbox_events USING btree (next_attempt_at, id) WHERE (published_at IS NULL);


--
-- Name: outbox_pending_monitor_fetch_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX outbox_pending_monitor_fetch_idx ON outbox_events USING btree (aggregate_id, ((payload ->> 'fetch_id'::text))) WHERE ((published_at IS NULL) AND ((aggregate_type)::text = 'monitor'::text) AND ((event_type)::text = ANY ((ARRAY['endpoint.fetch.json'::character varying, 'endpoint.fetch.html'::character varying])::text[])));


--
-- Name: processed_messages_processed_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX processed_messages_processed_at_idx ON processed_messages USING btree (processed_at);


--
-- Name: target_hostnames_refresh_lease_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX target_hostnames_refresh_lease_idx ON target_hostnames USING btree (refresh_lease_until) WHERE ((session_status)::text = 'refreshing'::text);


--
-- Name: user_contacts_active_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX user_contacts_active_idx ON user_contacts USING btree (user_id, is_active);


--
-- Name: user_contacts_user_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX user_contacts_user_id_idx ON user_contacts USING btree (user_id);


--
-- Name: contact_verifications contact_verifications_contact_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY contact_verifications
    ADD CONSTRAINT contact_verifications_contact_id_fkey FOREIGN KEY (contact_id) REFERENCES user_contacts(id) ON DELETE CASCADE;


--
-- Name: endpoint_fetches endpoint_fetches_endpoint_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY endpoint_fetches
    ADD CONSTRAINT endpoint_fetches_endpoint_id_fkey FOREIGN KEY (endpoint_id) REFERENCES monitored_endpoints(id) ON DELETE CASCADE;


--
-- Name: endpoint_fetches endpoint_fetches_monitor_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY endpoint_fetches
    ADD CONSTRAINT endpoint_fetches_monitor_id_fkey FOREIGN KEY (monitor_id) REFERENCES monitors(id) ON DELETE CASCADE;


--
-- Name: idempotency_keys idempotency_keys_monitor_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY idempotency_keys
    ADD CONSTRAINT idempotency_keys_monitor_id_fkey FOREIGN KEY (monitor_id) REFERENCES monitors(id) ON DELETE SET NULL;


--
-- Name: idempotency_keys idempotency_keys_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY idempotency_keys
    ADD CONSTRAINT idempotency_keys_user_id_fkey FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE SET NULL;


--
-- Name: monitor_fetch_evaluations monitor_fetch_evaluations_fetch_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY monitor_fetch_evaluations
    ADD CONSTRAINT monitor_fetch_evaluations_fetch_id_fkey FOREIGN KEY (fetch_id) REFERENCES endpoint_fetches(fetch_id) ON DELETE CASCADE;


--
-- Name: monitor_fetch_evaluations monitor_fetch_evaluations_monitor_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY monitor_fetch_evaluations
    ADD CONSTRAINT monitor_fetch_evaluations_monitor_id_fkey FOREIGN KEY (monitor_id) REFERENCES monitors(id) ON DELETE CASCADE;


--
-- Name: monitor_rules monitor_rules_monitor_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY monitor_rules
    ADD CONSTRAINT monitor_rules_monitor_id_fkey FOREIGN KEY (monitor_id) REFERENCES monitors(id) ON DELETE CASCADE;


--
-- Name: monitored_endpoints monitored_endpoints_target_hostname_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY monitored_endpoints
    ADD CONSTRAINT monitored_endpoints_target_hostname_id_fkey FOREIGN KEY (target_hostname_id) REFERENCES target_hostnames(id) ON DELETE RESTRICT;


--
-- Name: monitors monitors_endpoint_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY monitors
    ADD CONSTRAINT monitors_endpoint_id_fkey FOREIGN KEY (endpoint_id) REFERENCES monitored_endpoints(id) ON DELETE CASCADE;


--
-- Name: monitors monitors_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY monitors
    ADD CONSTRAINT monitors_user_id_fkey FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;


--
-- Name: notifications notifications_monitor_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY notifications
    ADD CONSTRAINT notifications_monitor_id_fkey FOREIGN KEY (monitor_id) REFERENCES monitors(id) ON DELETE CASCADE;


--
-- Name: notifications notifications_user_contact_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY notifications
    ADD CONSTRAINT notifications_user_contact_id_fkey FOREIGN KEY (user_contact_id) REFERENCES user_contacts(id) ON DELETE SET NULL;


--
-- Name: operational_notifications operational_notifications_dependency_name_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY operational_notifications
    ADD CONSTRAINT operational_notifications_dependency_name_fkey FOREIGN KEY (dependency_name) REFERENCES service_dependencies(name);


--
-- Name: user_contacts user_contacts_contact_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY user_contacts
    ADD CONSTRAINT user_contacts_contact_id_fkey FOREIGN KEY (contact_id) REFERENCES contacts(id) ON DELETE CASCADE;


--
-- Name: user_contacts user_contacts_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY user_contacts
    ADD CONSTRAINT user_contacts_user_id_fkey FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;


--
-- PostgreSQL database dump complete
--


