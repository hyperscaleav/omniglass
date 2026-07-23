-- migrate:up

-- Squashed schema: the single init that replaces the per-slice migration
-- chain (dbmate keys on version, so this is a one-time collapse of the whole
-- history into the current schema; the deleted migrations are gone from the
-- tree). Generated from a pg_dump of the full applied chain, minus the dbmate
-- schema_migrations table and the psql meta-commands. Pure DDL, no seed rows.

--
-- PostgreSQL database dump
--


-- Dumped from database version 18.4 (Debian 18.4-1.pgdg13+1)
-- Dumped by pg_dump version 18.4 (Debian 18.4-1.pgdg13+1)

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET transaction_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

--
-- Name: assert_owner_grant_exists(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.assert_owner_grant_exists() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
begin
    if not exists (
        select 1 from principal_grant
        where role_id = (select id from role where name = 'owner') and scope_kind = 'all'
    ) then
        raise exception 'at least one owner grant must remain'
            using errcode = 'OG001';
    end if;
    return null;
end;
$$;


--
-- Name: principal_label(uuid); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.principal_label(pid uuid) RETURNS text
    LANGUAGE sql STABLE
    AS $$
    select coalesce(
        (select username from human where principal_id = pid),
        (select label from service where principal_id = pid)
    );
$$;


SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: alarm; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.alarm (
    id uuid DEFAULT uuidv7() NOT NULL,
    severity text NOT NULL,
    message text DEFAULT ''::text NOT NULL,
    raised_at timestamp with time zone DEFAULT now() NOT NULL,
    cleared_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    component_id uuid NOT NULL,
    CONSTRAINT alarm_severity_check CHECK ((severity = ANY (ARRAY['info'::text, 'warning'::text, 'critical'::text])))
);


--
-- Name: alarm_capability; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.alarm_capability (
    id uuid DEFAULT uuidv7() NOT NULL,
    alarm_id uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    capability_id uuid NOT NULL
);


--
-- Name: audit_log; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.audit_log (
    id uuid DEFAULT uuidv7() NOT NULL,
    ts timestamp with time zone DEFAULT now() NOT NULL,
    actor_principal_id uuid,
    verb text NOT NULL,
    resource text NOT NULL,
    resource_id text,
    old jsonb,
    new jsonb,
    real_actor_principal_id uuid,
    actor_username text,
    real_actor_username text
);


--
-- Name: blob; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.blob (
    sha256 text NOT NULL,
    bytes bytea NOT NULL,
    size bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: capability; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.capability (
    name text CONSTRAINT capability_name_not_null NOT NULL,
    display_name text NOT NULL,
    official boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    id uuid DEFAULT uuidv7() CONSTRAINT capability_id_not_null NOT NULL
);


--
-- Name: component; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.component (
    id uuid DEFAULT uuidv7() NOT NULL,
    name text NOT NULL,
    display_name text,
    parent_id uuid,
    location_id uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    product_id uuid
);


--
-- Name: component_capability; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.component_capability (
    id uuid DEFAULT uuidv7() NOT NULL,
    present boolean DEFAULT true NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    component_id uuid NOT NULL,
    capability_id uuid NOT NULL
);


--
-- Name: credential; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.credential (
    id uuid DEFAULT uuidv7() NOT NULL,
    principal_id uuid NOT NULL,
    kind text NOT NULL,
    secret_hash bytea NOT NULL,
    prefix text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    last_used_at timestamp with time zone,
    expires_at timestamp with time zone,
    purpose text,
    description text,
    user_agent text,
    client_ip text,
    CONSTRAINT credential_kind_check CHECK ((kind = ANY (ARRAY['bearer'::text, 'password'::text])))
);


--
-- Name: driver; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.driver (
    name text CONSTRAINT driver_name_not_null NOT NULL,
    display_name text NOT NULL,
    version text DEFAULT ''::text NOT NULL,
    official boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    id uuid DEFAULT uuidv7() CONSTRAINT driver_id_not_null NOT NULL
);


--
-- Name: event; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.event (
    id bigint NOT NULL,
    ts timestamp with time zone DEFAULT now() NOT NULL,
    owner_kind text NOT NULL,
    instance text DEFAULT ''::text NOT NULL,
    message text DEFAULT ''::text NOT NULL,
    attributes jsonb,
    provenance text DEFAULT 'observed'::text NOT NULL,
    source text DEFAULT ''::text NOT NULL,
    source_rule text,
    source_rule_version bigint,
    component_id uuid,
    system_id uuid,
    location_id uuid,
    node_id uuid,
    property_id uuid NOT NULL,
    CONSTRAINT event_owner_arc_check CHECK ((((owner_kind = 'component'::text) AND (component_id IS NOT NULL) AND (system_id IS NULL) AND (location_id IS NULL) AND (node_id IS NULL)) OR ((owner_kind = 'system'::text) AND (system_id IS NOT NULL) AND (component_id IS NULL) AND (location_id IS NULL) AND (node_id IS NULL)) OR ((owner_kind = 'location'::text) AND (location_id IS NOT NULL) AND (component_id IS NULL) AND (system_id IS NULL) AND (node_id IS NULL)) OR ((owner_kind = 'node'::text) AND (node_id IS NOT NULL) AND (component_id IS NULL) AND (system_id IS NULL) AND (location_id IS NULL)))),
    CONSTRAINT event_owner_kind_check CHECK ((owner_kind = ANY (ARRAY['component'::text, 'system'::text, 'location'::text, 'node'::text]))),
    CONSTRAINT event_provenance_check CHECK ((provenance = ANY (ARRAY['observed'::text, 'calculated'::text, 'intended'::text, 'declared'::text])))
);


--
-- Name: event_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

ALTER TABLE public.event ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME public.event_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);


--
-- Name: file; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.file (
    id uuid DEFAULT uuidv7() NOT NULL,
    name text NOT NULL,
    content_type text NOT NULL,
    size bigint NOT NULL,
    sha256 text NOT NULL,
    sensitive boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: human; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.human (
    principal_id uuid NOT NULL,
    username text NOT NULL,
    email text,
    display_name text,
    failed_login_count integer DEFAULT 0 NOT NULL,
    locked_until timestamp with time zone,
    must_change_password boolean DEFAULT false NOT NULL,
    avatar text,
    avatar_updated_at timestamp with time zone
);


--
-- Name: impersonation_session; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.impersonation_session (
    id uuid DEFAULT uuidv7() NOT NULL,
    token_hash bytea NOT NULL,
    target_principal_id uuid NOT NULL,
    real_actor_principal_id uuid NOT NULL,
    mode text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    revoked_at timestamp with time zone,
    CONSTRAINT impersonation_session_mode_check CHECK ((mode = ANY (ARRAY['view_as'::text, 'act_as'::text])))
);


--
-- Name: interface; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.interface (
    id uuid DEFAULT uuidv7() NOT NULL,
    name text NOT NULL,
    params jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    component uuid,
    node_name uuid,
    type uuid NOT NULL
);


--
-- Name: interface_type; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.interface_type (
    name text NOT NULL,
    official boolean DEFAULT false NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    built boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    id uuid DEFAULT uuidv7() CONSTRAINT interface_type_id_not_null NOT NULL
);


--
-- Name: location; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.location (
    id uuid DEFAULT uuidv7() NOT NULL,
    name text NOT NULL,
    display_name text,
    parent_id uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    location_type uuid NOT NULL
);


--
-- Name: location_type; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.location_type (
    name text CONSTRAINT location_type_name_not_null NOT NULL,
    official boolean DEFAULT false NOT NULL,
    display_name text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    icon text DEFAULT 'map-pin'::text NOT NULL,
    allowed_parent_types text[] DEFAULT '{}'::text[] NOT NULL,
    id uuid DEFAULT uuidv7() CONSTRAINT location_type_id_not_null NOT NULL
);


--
-- Name: location_type_property; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.location_type_property (
    id uuid DEFAULT uuidv7() NOT NULL,
    default_value jsonb,
    required boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    property_id uuid NOT NULL,
    location_type_id uuid NOT NULL
);


--
-- Name: metric; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.metric (
    id bigint CONSTRAINT metric_id_not_null NOT NULL,
    ts timestamp with time zone DEFAULT now() CONSTRAINT metric_ts_not_null NOT NULL,
    owner_kind text CONSTRAINT metric_owner_kind_not_null NOT NULL,
    instance text DEFAULT ''::text CONSTRAINT metric_instance_not_null NOT NULL,
    value double precision CONSTRAINT metric_value_not_null NOT NULL,
    provenance text DEFAULT 'observed'::text CONSTRAINT metric_provenance_not_null NOT NULL,
    source text DEFAULT ''::text CONSTRAINT metric_source_not_null NOT NULL,
    source_rule text,
    source_rule_version bigint,
    event_id bigint,
    component_id uuid,
    system_id uuid,
    location_id uuid,
    node_id uuid,
    property_id uuid CONSTRAINT metric_property_id_not_null NOT NULL,
    CONSTRAINT metric_lineage_check CHECK ((((provenance = 'observed'::text) AND (event_id IS NULL)) OR ((provenance = 'calculated'::text) AND (source_rule IS NOT NULL) AND (event_id IS NULL)) OR ((provenance = 'intended'::text) AND (event_id IS NOT NULL) AND (source_rule IS NULL)) OR ((provenance = 'declared'::text) AND (source_rule IS NULL) AND (event_id IS NULL)))),
    CONSTRAINT metric_owner_arc_check CHECK ((((owner_kind = 'component'::text) AND (component_id IS NOT NULL) AND (system_id IS NULL) AND (location_id IS NULL) AND (node_id IS NULL)) OR ((owner_kind = 'system'::text) AND (system_id IS NOT NULL) AND (component_id IS NULL) AND (location_id IS NULL) AND (node_id IS NULL)) OR ((owner_kind = 'location'::text) AND (location_id IS NOT NULL) AND (component_id IS NULL) AND (system_id IS NULL) AND (node_id IS NULL)) OR ((owner_kind = 'node'::text) AND (node_id IS NOT NULL) AND (component_id IS NULL) AND (system_id IS NULL) AND (location_id IS NULL)))),
    CONSTRAINT metric_owner_kind_check CHECK ((owner_kind = ANY (ARRAY['component'::text, 'system'::text, 'location'::text, 'node'::text]))),
    CONSTRAINT metric_provenance_check CHECK ((provenance = ANY (ARRAY['observed'::text, 'calculated'::text, 'intended'::text, 'declared'::text])))
);


--
-- Name: metric_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

ALTER TABLE public.metric ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME public.metric_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);


--
-- Name: node; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.node (
    principal_id uuid NOT NULL,
    name text NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    last_heartbeat_at timestamp with time zone,
    enrolled_at timestamp with time zone,
    labels jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    display_name text,
    location_id uuid,
    CONSTRAINT node_name_subject_safe_check CHECK ((name ~ '^[^.*> \t\n\r]+$'::text))
);


--
-- Name: platform_setting; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.platform_setting (
    key text NOT NULL,
    value jsonb DEFAULT '{}'::jsonb NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: principal; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.principal (
    id uuid DEFAULT uuidv7() NOT NULL,
    kind text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    active boolean DEFAULT true NOT NULL,
    archived_at timestamp with time zone,
    CONSTRAINT principal_kind_check CHECK ((kind = ANY (ARRAY['human'::text, 'service'::text, 'node'::text, 'agent'::text])))
);


--
-- Name: principal_grant; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.principal_grant (
    id uuid DEFAULT uuidv7() NOT NULL,
    principal_id uuid,
    scope_kind text NOT NULL,
    scope_id text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    scope_op text DEFAULT 'subtree'::text NOT NULL,
    group_id uuid,
    role_id uuid NOT NULL,
    CONSTRAINT principal_grant_scope_kind_check CHECK ((scope_kind = ANY (ARRAY['all'::text, 'location'::text, 'system'::text, 'component'::text, 'group'::text]))),
    CONSTRAINT principal_grant_scope_op_check CHECK ((scope_op = ANY (ARRAY['subtree'::text, 'subtree_excl_root'::text, 'self'::text]))),
    CONSTRAINT principal_grant_target_ck CHECK ((num_nonnulls(principal_id, group_id) = 1))
);


--
-- Name: principal_group; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.principal_group (
    id uuid DEFAULT uuidv7() NOT NULL,
    name text NOT NULL,
    display_name text,
    description text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: principal_group_member; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.principal_group_member (
    group_id uuid NOT NULL,
    principal_id uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: product; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.product (
    name text CONSTRAINT product_name_not_null NOT NULL,
    display_name text NOT NULL,
    kind text DEFAULT 'device'::text NOT NULL,
    official boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    id uuid DEFAULT uuidv7() CONSTRAINT product_id_not_null NOT NULL,
    vendor_id uuid,
    parent_product_id uuid,
    driver_id uuid,
    CONSTRAINT product_kind_check CHECK ((kind = ANY (ARRAY['device'::text, 'app'::text, 'service'::text, 'vm'::text])))
);


--
-- Name: product_capability; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.product_capability (
    id uuid DEFAULT uuidv7() NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    product_id uuid NOT NULL,
    capability_id uuid NOT NULL
);


--
-- Name: product_property; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.product_property (
    id uuid DEFAULT uuidv7() NOT NULL,
    default_value jsonb,
    required boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    product_id uuid NOT NULL,
    property_id uuid NOT NULL
);


--
-- Name: property; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.property (
    name text CONSTRAINT property_name_not_null NOT NULL,
    display_name text,
    kind text,
    data_type text CONSTRAINT property_value_type_not_null NOT NULL,
    unit text,
    "precision" integer,
    fusion_policy jsonb,
    validation jsonb,
    description text DEFAULT ''::text CONSTRAINT property_description_not_null NOT NULL,
    registered_at timestamp with time zone DEFAULT now() CONSTRAINT property_registered_at_not_null NOT NULL,
    official boolean DEFAULT false NOT NULL,
    id uuid DEFAULT uuidv7() CONSTRAINT property_id_not_null NOT NULL,
    CONSTRAINT property_kind_check CHECK ((kind = ANY (ARRAY['metric'::text, 'state'::text, 'log'::text]))),
    CONSTRAINT property_data_type_check CHECK ((data_type = ANY (ARRAY['string'::text, 'int'::text, 'float'::text, 'bool'::text, 'json'::text])))
);


--
-- Name: property_value; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.property_value (
    id uuid DEFAULT uuidv7() NOT NULL,
    owner_kind text NOT NULL,
    instance text DEFAULT ''::text NOT NULL,
    provenance text DEFAULT 'declared'::text NOT NULL,
    value jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    component_id uuid,
    system_id uuid,
    location_id uuid,
    node_id uuid,
    property_id uuid NOT NULL,
    CONSTRAINT property_value_owner_arc_check CHECK ((((owner_kind = 'component'::text) AND (component_id IS NOT NULL) AND (system_id IS NULL) AND (location_id IS NULL) AND (node_id IS NULL)) OR ((owner_kind = 'system'::text) AND (system_id IS NOT NULL) AND (component_id IS NULL) AND (location_id IS NULL) AND (node_id IS NULL)) OR ((owner_kind = 'location'::text) AND (location_id IS NOT NULL) AND (component_id IS NULL) AND (system_id IS NULL) AND (node_id IS NULL)) OR ((owner_kind = 'node'::text) AND (node_id IS NOT NULL) AND (component_id IS NULL) AND (system_id IS NULL) AND (location_id IS NULL)))),
    CONSTRAINT property_value_owner_kind_check CHECK ((owner_kind = ANY (ARRAY['component'::text, 'system'::text, 'location'::text, 'node'::text]))),
    CONSTRAINT property_value_provenance_check CHECK ((provenance = ANY (ARRAY['observed'::text, 'calculated'::text, 'intended'::text, 'declared'::text])))
);


--
-- Name: role; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.role (
    name text CONSTRAINT role_id_not_null NOT NULL,
    official boolean DEFAULT false NOT NULL,
    permissions text[] DEFAULT '{}'::text[] NOT NULL,
    inherits text[] DEFAULT '{}'::text[] NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    display_name text,
    description text,
    id uuid DEFAULT uuidv7() CONSTRAINT role_id_not_null1 NOT NULL
);


--
-- Name: system_role_assignment; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.system_role_assignment (
    id uuid DEFAULT uuidv7() NOT NULL,
    role_id uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    component_id uuid NOT NULL,
    system_id uuid NOT NULL
);


--
-- Name: system_role_capability; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.system_role_capability (
    id uuid DEFAULT uuidv7() NOT NULL,
    role_id uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    capability_id uuid NOT NULL
);


--
--



--
-- Name: secret; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.secret (
    id uuid DEFAULT uuidv7() NOT NULL,
    name text NOT NULL,
    owner_kind text NOT NULL,
    value jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    admin_sensitive boolean DEFAULT true NOT NULL,
    component_id uuid,
    system_id uuid,
    location_id uuid,
    secret_type uuid NOT NULL,
    CONSTRAINT secret_owner_arc CHECK ((((owner_kind = 'platform'::text) AND (component_id IS NULL) AND (system_id IS NULL) AND (location_id IS NULL)) OR ((owner_kind = 'component'::text) AND (component_id IS NOT NULL) AND (system_id IS NULL) AND (location_id IS NULL)) OR ((owner_kind = 'system'::text) AND (system_id IS NOT NULL) AND (component_id IS NULL) AND (location_id IS NULL)) OR ((owner_kind = 'location'::text) AND (location_id IS NOT NULL) AND (component_id IS NULL) AND (system_id IS NULL)))),
    CONSTRAINT secret_owner_kind_check CHECK ((owner_kind = ANY (ARRAY['platform'::text, 'component'::text, 'system'::text, 'location'::text])))
);


--
-- Name: secret_type; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.secret_type (
    name text CONSTRAINT secret_type_name_not_null NOT NULL,
    official boolean DEFAULT false NOT NULL,
    display_name text NOT NULL,
    schema jsonb DEFAULT '[]'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    default_admin_sensitive boolean DEFAULT true NOT NULL,
    id uuid DEFAULT uuidv7() CONSTRAINT secret_type_id_not_null NOT NULL
);


--
-- Name: service; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.service (
    principal_id uuid NOT NULL,
    label text NOT NULL
);


--
-- Name: setting_override; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.setting_override (
    id uuid DEFAULT uuidv7() NOT NULL,
    scope text NOT NULL,
    principal_id uuid,
    namespace text NOT NULL,
    doc jsonb DEFAULT '{}'::jsonb NOT NULL,
    locks jsonb DEFAULT '[]'::jsonb NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_by uuid,
    CONSTRAINT setting_override_scope_check CHECK ((scope = 'platform'::text))
);


--
-- Name: standard; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.standard (
    name text CONSTRAINT standard_name_not_null NOT NULL,
    official boolean DEFAULT false CONSTRAINT standard_official_not_null NOT NULL,
    display_name text CONSTRAINT standard_display_name_not_null NOT NULL,
    created_at timestamp with time zone DEFAULT now() CONSTRAINT standard_created_at_not_null NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    id uuid DEFAULT uuidv7() CONSTRAINT standard_id_not_null NOT NULL,
    parent_standard_id uuid
);


--
-- Name: standard_property; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.standard_property (
    id uuid DEFAULT uuidv7() NOT NULL,
    default_value jsonb,
    required boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    standard_id uuid NOT NULL,
    property_id uuid NOT NULL
);


--
-- Name: state; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.state (
    id bigint CONSTRAINT state_id_not_null NOT NULL,
    ts timestamp with time zone DEFAULT now() CONSTRAINT state_ts_not_null NOT NULL,
    owner_kind text CONSTRAINT state_owner_kind_not_null NOT NULL,
    instance text DEFAULT ''::text CONSTRAINT state_instance_not_null NOT NULL,
    value text CONSTRAINT state_value_not_null NOT NULL,
    value_json jsonb,
    provenance text DEFAULT 'observed'::text CONSTRAINT state_provenance_not_null NOT NULL,
    source text DEFAULT ''::text CONSTRAINT state_source_not_null NOT NULL,
    source_rule text,
    source_rule_version bigint,
    event_id bigint,
    component_id uuid,
    system_id uuid,
    location_id uuid,
    node_id uuid,
    property_id uuid CONSTRAINT state_property_id_not_null NOT NULL,
    CONSTRAINT state_lineage_check CHECK ((((provenance = 'observed'::text) AND (event_id IS NULL)) OR ((provenance = 'calculated'::text) AND (source_rule IS NOT NULL) AND (event_id IS NULL)) OR ((provenance = 'intended'::text) AND (event_id IS NOT NULL) AND (source_rule IS NULL)) OR ((provenance = 'declared'::text) AND (source_rule IS NULL) AND (event_id IS NULL)))),
    CONSTRAINT state_owner_arc_check CHECK ((((owner_kind = 'component'::text) AND (component_id IS NOT NULL) AND (system_id IS NULL) AND (location_id IS NULL) AND (node_id IS NULL)) OR ((owner_kind = 'system'::text) AND (system_id IS NOT NULL) AND (component_id IS NULL) AND (location_id IS NULL) AND (node_id IS NULL)) OR ((owner_kind = 'location'::text) AND (location_id IS NOT NULL) AND (component_id IS NULL) AND (system_id IS NULL) AND (node_id IS NULL)) OR ((owner_kind = 'node'::text) AND (node_id IS NOT NULL) AND (component_id IS NULL) AND (system_id IS NULL) AND (location_id IS NULL)))),
    CONSTRAINT state_owner_kind_check CHECK ((owner_kind = ANY (ARRAY['component'::text, 'system'::text, 'location'::text, 'node'::text]))),
    CONSTRAINT state_provenance_check CHECK ((provenance = ANY (ARRAY['observed'::text, 'calculated'::text, 'intended'::text, 'declared'::text])))
);


--
-- Name: state_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

ALTER TABLE public.state ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME public.state_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);


--
-- Name: system; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.system (
    id uuid DEFAULT uuidv7() NOT NULL,
    name text NOT NULL,
    display_name text,
    parent_id uuid,
    location_id uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    standard_id uuid
);


--
-- Name: system_member; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.system_member (
    id uuid DEFAULT uuidv7() NOT NULL,
    is_primary boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    component_id uuid NOT NULL,
    system_id uuid NOT NULL
);


--
-- Name: system_role; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.system_role (
    id uuid DEFAULT uuidv7() NOT NULL,
    owner_kind text NOT NULL,
    name text NOT NULL,
    display_name text NOT NULL,
    quorum integer DEFAULT 1 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    impact text DEFAULT 'degraded'::text NOT NULL,
    system_id uuid,
    standard_id uuid,
    CONSTRAINT system_role_impact_check CHECK ((impact = ANY (ARRAY['outage'::text, 'degraded'::text, 'none'::text]))),
    CONSTRAINT system_role_owner_kind_check CHECK ((owner_kind = ANY (ARRAY['standard'::text, 'system'::text]))),
    CONSTRAINT system_role_quorum_check CHECK ((quorum >= 1))
);


--
-- Name: tag; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.tag (
    id uuid DEFAULT uuidv7() NOT NULL,
    name text NOT NULL,
    applies_to text[] DEFAULT '{}'::text[] NOT NULL,
    propagates boolean DEFAULT true NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    allowed_values text[] DEFAULT '{}'::text[] NOT NULL
);


--
-- Name: tag_binding; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.tag_binding (
    id uuid DEFAULT uuidv7() NOT NULL,
    tag_id uuid NOT NULL,
    owner_kind text NOT NULL,
    value text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    node_id uuid,
    component_id uuid,
    system_id uuid,
    location_id uuid,
    CONSTRAINT tag_binding_owner_arc CHECK ((((owner_kind = 'platform'::text) AND (component_id IS NULL) AND (system_id IS NULL) AND (location_id IS NULL) AND (node_id IS NULL)) OR ((owner_kind = 'component'::text) AND (component_id IS NOT NULL) AND (system_id IS NULL) AND (location_id IS NULL) AND (node_id IS NULL)) OR ((owner_kind = 'system'::text) AND (system_id IS NOT NULL) AND (component_id IS NULL) AND (location_id IS NULL) AND (node_id IS NULL)) OR ((owner_kind = 'location'::text) AND (location_id IS NOT NULL) AND (component_id IS NULL) AND (system_id IS NULL) AND (node_id IS NULL)) OR ((owner_kind = 'node'::text) AND (node_id IS NOT NULL) AND (component_id IS NULL) AND (system_id IS NULL) AND (location_id IS NULL)))),
    CONSTRAINT tag_binding_owner_kind_check CHECK ((owner_kind = ANY (ARRAY['platform'::text, 'component'::text, 'system'::text, 'location'::text, 'node'::text])))
);


--
-- Name: task; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.task (
    id text NOT NULL,
    display_name text DEFAULT ''::text NOT NULL,
    mode text NOT NULL,
    interface_id uuid NOT NULL,
    spec jsonb DEFAULT '{}'::jsonb NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT task_mode_check CHECK ((mode = ANY (ARRAY['poll'::text, 'listen'::text])))
);


--
-- Name: variable; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.variable (
    id uuid DEFAULT uuidv7() NOT NULL,
    name text NOT NULL,
    value_type text NOT NULL,
    owner_kind text NOT NULL,
    value jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    component_id uuid,
    system_id uuid,
    location_id uuid,
    CONSTRAINT variable_owner_arc CHECK ((((owner_kind = 'platform'::text) AND (component_id IS NULL) AND (system_id IS NULL) AND (location_id IS NULL)) OR ((owner_kind = 'component'::text) AND (component_id IS NOT NULL) AND (system_id IS NULL) AND (location_id IS NULL)) OR ((owner_kind = 'system'::text) AND (system_id IS NOT NULL) AND (component_id IS NULL) AND (location_id IS NULL)) OR ((owner_kind = 'location'::text) AND (location_id IS NOT NULL) AND (component_id IS NULL) AND (system_id IS NULL)))),
    CONSTRAINT variable_owner_kind_check CHECK ((owner_kind = ANY (ARRAY['platform'::text, 'component'::text, 'system'::text, 'location'::text]))),
    CONSTRAINT variable_value_type_check CHECK ((value_type = ANY (ARRAY['string'::text, 'int'::text, 'float'::text, 'bool'::text, 'json'::text])))
);


--
-- Name: vendor; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.vendor (
    name text CONSTRAINT vendor_name_not_null NOT NULL,
    display_name text CONSTRAINT vendor_display_name_not_null NOT NULL,
    icon text DEFAULT ''::text CONSTRAINT vendor_icon_not_null NOT NULL,
    support_phone text DEFAULT ''::text CONSTRAINT vendor_support_phone_not_null NOT NULL,
    website text DEFAULT ''::text CONSTRAINT vendor_website_not_null NOT NULL,
    official boolean DEFAULT false CONSTRAINT vendor_official_not_null NOT NULL,
    created_at timestamp with time zone DEFAULT now() CONSTRAINT vendor_created_at_not_null NOT NULL,
    updated_at timestamp with time zone DEFAULT now() CONSTRAINT vendor_updated_at_not_null NOT NULL,
    kind text DEFAULT 'manufacturer'::text NOT NULL,
    id uuid DEFAULT uuidv7() CONSTRAINT vendor_id_not_null NOT NULL,
    CONSTRAINT vendor_kind_check CHECK ((kind = ANY (ARRAY['manufacturer'::text, 'integrator'::text, 'developer'::text])))
);


--
-- Name: alarm_capability alarm_capability_alarm_id_capability_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.alarm_capability
    ADD CONSTRAINT alarm_capability_alarm_id_capability_id_key UNIQUE (alarm_id, capability_id);


--
-- Name: alarm_capability alarm_capability_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.alarm_capability
    ADD CONSTRAINT alarm_capability_pkey PRIMARY KEY (id);


--
-- Name: alarm alarm_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.alarm
    ADD CONSTRAINT alarm_pkey PRIMARY KEY (id);


--
-- Name: audit_log audit_log_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.audit_log
    ADD CONSTRAINT audit_log_pkey PRIMARY KEY (id);


--
-- Name: blob blob_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.blob
    ADD CONSTRAINT blob_pkey PRIMARY KEY (sha256);


--
-- Name: capability capability_name_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.capability
    ADD CONSTRAINT capability_name_key UNIQUE (name);


--
-- Name: capability capability_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.capability
    ADD CONSTRAINT capability_pkey PRIMARY KEY (id);


--
-- Name: component_capability component_capability_component_id_capability_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.component_capability
    ADD CONSTRAINT component_capability_component_id_capability_id_key UNIQUE (component_id, capability_id);


--
-- Name: component_capability component_capability_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.component_capability
    ADD CONSTRAINT component_capability_pkey PRIMARY KEY (id);


--
-- Name: component component_name_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.component
    ADD CONSTRAINT component_name_key UNIQUE (name);


--
-- Name: component component_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.component
    ADD CONSTRAINT component_pkey PRIMARY KEY (id);


--
-- Name: credential credential_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.credential
    ADD CONSTRAINT credential_pkey PRIMARY KEY (id);


--
-- Name: driver driver_name_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.driver
    ADD CONSTRAINT driver_name_key UNIQUE (name);


--
-- Name: driver driver_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.driver
    ADD CONSTRAINT driver_pkey PRIMARY KEY (id);


--
-- Name: event event_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.event
    ADD CONSTRAINT event_pkey PRIMARY KEY (id);


--
-- Name: file file_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.file
    ADD CONSTRAINT file_pkey PRIMARY KEY (id);


--
-- Name: human human_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.human
    ADD CONSTRAINT human_pkey PRIMARY KEY (principal_id);


--
-- Name: human human_username_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.human
    ADD CONSTRAINT human_username_key UNIQUE (username);


--
-- Name: impersonation_session impersonation_session_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.impersonation_session
    ADD CONSTRAINT impersonation_session_pkey PRIMARY KEY (id);


--
-- Name: impersonation_session impersonation_session_token_hash_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.impersonation_session
    ADD CONSTRAINT impersonation_session_token_hash_key UNIQUE (token_hash);


--
-- Name: interface interface_component_name_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.interface
    ADD CONSTRAINT interface_component_name_key UNIQUE (component, name);


--
-- Name: interface interface_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.interface
    ADD CONSTRAINT interface_pkey PRIMARY KEY (id);


--
-- Name: interface_type interface_type_name_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.interface_type
    ADD CONSTRAINT interface_type_name_key UNIQUE (name);


--
-- Name: interface_type interface_type_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.interface_type
    ADD CONSTRAINT interface_type_pkey PRIMARY KEY (id);


--
-- Name: location location_name_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.location
    ADD CONSTRAINT location_name_key UNIQUE (name);


--
-- Name: location location_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.location
    ADD CONSTRAINT location_pkey PRIMARY KEY (id);


--
-- Name: location_type location_type_name_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.location_type
    ADD CONSTRAINT location_type_name_key UNIQUE (name);


--
-- Name: location_type location_type_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.location_type
    ADD CONSTRAINT location_type_pkey PRIMARY KEY (id);


--
-- Name: location_type_property location_type_property_location_type_id_property_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.location_type_property
    ADD CONSTRAINT location_type_property_location_type_id_property_id_key UNIQUE (location_type_id, property_id);


--
-- Name: location_type_property location_type_property_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.location_type_property
    ADD CONSTRAINT location_type_property_pkey PRIMARY KEY (id);


--
-- Name: metric metric_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.metric
    ADD CONSTRAINT metric_pkey PRIMARY KEY (id);


--
-- Name: node node_name_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.node
    ADD CONSTRAINT node_name_key UNIQUE (name);


--
-- Name: node node_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.node
    ADD CONSTRAINT node_pkey PRIMARY KEY (principal_id);


--
-- Name: platform_setting platform_setting_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.platform_setting
    ADD CONSTRAINT platform_setting_pkey PRIMARY KEY (key);


--
-- Name: principal_grant principal_grant_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.principal_grant
    ADD CONSTRAINT principal_grant_pkey PRIMARY KEY (id);


--
-- Name: principal_group_member principal_group_member_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.principal_group_member
    ADD CONSTRAINT principal_group_member_pkey PRIMARY KEY (group_id, principal_id);


--
-- Name: principal_group principal_group_name_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.principal_group
    ADD CONSTRAINT principal_group_name_key UNIQUE (name);


--
-- Name: principal_group principal_group_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.principal_group
    ADD CONSTRAINT principal_group_pkey PRIMARY KEY (id);


--
-- Name: principal principal_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.principal
    ADD CONSTRAINT principal_pkey PRIMARY KEY (id);


--
-- Name: product_capability product_capability_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.product_capability
    ADD CONSTRAINT product_capability_pkey PRIMARY KEY (id);


--
-- Name: product_capability product_capability_product_id_capability_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.product_capability
    ADD CONSTRAINT product_capability_product_id_capability_id_key UNIQUE (product_id, capability_id);


--
-- Name: product product_name_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.product
    ADD CONSTRAINT product_name_key UNIQUE (name);


--
-- Name: product product_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.product
    ADD CONSTRAINT product_pkey PRIMARY KEY (id);


--
-- Name: product_property product_property_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.product_property
    ADD CONSTRAINT product_property_pkey PRIMARY KEY (id);


--
-- Name: product_property product_property_product_id_property_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.product_property
    ADD CONSTRAINT product_property_product_id_property_id_key UNIQUE (product_id, property_id);


--
-- Name: property property_handle_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.property
    ADD CONSTRAINT property_handle_key UNIQUE (name);


--
-- Name: property property_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.property
    ADD CONSTRAINT property_pkey PRIMARY KEY (id);


--
-- Name: property_value property_value_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.property_value
    ADD CONSTRAINT property_value_pkey PRIMARY KEY (id);


--
-- Name: property_value property_value_series_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.property_value
    ADD CONSTRAINT property_value_series_key UNIQUE NULLS NOT DISTINCT (owner_kind, component_id, system_id, location_id, node_id, property_id, instance, provenance);


--
-- Name: system_role_assignment system_role_assignment_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.system_role_assignment
    ADD CONSTRAINT system_role_assignment_pkey PRIMARY KEY (id);


--
-- Name: system_role_assignment system_role_assignment_system_id_role_id_component_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.system_role_assignment
    ADD CONSTRAINT system_role_assignment_system_id_role_id_component_id_key UNIQUE (system_id, role_id, component_id);


--
-- Name: system_role_capability system_role_capability_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.system_role_capability
    ADD CONSTRAINT system_role_capability_pkey PRIMARY KEY (id);


--
-- Name: system_role_capability system_role_capability_role_id_capability_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.system_role_capability
    ADD CONSTRAINT system_role_capability_role_id_capability_id_key UNIQUE (role_id, capability_id);


--
-- Name: role role_name_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.role
    ADD CONSTRAINT role_name_key UNIQUE (name);


--
-- Name: role role_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.role
    ADD CONSTRAINT role_pkey PRIMARY KEY (id);


--
--



--
-- Name: secret secret_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.secret
    ADD CONSTRAINT secret_pkey PRIMARY KEY (id);


--
-- Name: secret_type secret_type_name_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.secret_type
    ADD CONSTRAINT secret_type_name_key UNIQUE (name);


--
-- Name: secret_type secret_type_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.secret_type
    ADD CONSTRAINT secret_type_pkey PRIMARY KEY (id);


--
-- Name: service service_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.service
    ADD CONSTRAINT service_pkey PRIMARY KEY (principal_id);


--
-- Name: setting_override setting_override_identity; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.setting_override
    ADD CONSTRAINT setting_override_identity UNIQUE NULLS NOT DISTINCT (scope, principal_id, namespace);


--
-- Name: setting_override setting_override_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.setting_override
    ADD CONSTRAINT setting_override_pkey PRIMARY KEY (id);


--
-- Name: standard standard_name_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.standard
    ADD CONSTRAINT standard_name_key UNIQUE (name);


--
-- Name: standard standard_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.standard
    ADD CONSTRAINT standard_pkey PRIMARY KEY (id);


--
-- Name: standard_property standard_property_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.standard_property
    ADD CONSTRAINT standard_property_pkey PRIMARY KEY (id);


--
-- Name: standard_property standard_property_standard_id_property_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.standard_property
    ADD CONSTRAINT standard_property_standard_id_property_id_key UNIQUE (standard_id, property_id);


--
-- Name: state state_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.state
    ADD CONSTRAINT state_pkey PRIMARY KEY (id);


--
-- Name: system_member system_member_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.system_member
    ADD CONSTRAINT system_member_pkey PRIMARY KEY (id);


--
-- Name: system_member system_member_system_id_component_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.system_member
    ADD CONSTRAINT system_member_system_id_component_id_key UNIQUE (system_id, component_id);


--
-- Name: system system_name_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.system
    ADD CONSTRAINT system_name_key UNIQUE (name);


--
-- Name: system system_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.system
    ADD CONSTRAINT system_pkey PRIMARY KEY (id);


--
-- Name: system_role system_role_name_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.system_role
    ADD CONSTRAINT system_role_name_key UNIQUE NULLS NOT DISTINCT (owner_kind, standard_id, system_id, name);


--
-- Name: system_role system_role_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.system_role
    ADD CONSTRAINT system_role_pkey PRIMARY KEY (id);


--
-- Name: tag_binding tag_binding_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tag_binding
    ADD CONSTRAINT tag_binding_pkey PRIMARY KEY (id);


--
-- Name: tag tag_name_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tag
    ADD CONSTRAINT tag_name_key UNIQUE (name);


--
-- Name: tag tag_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tag
    ADD CONSTRAINT tag_pkey PRIMARY KEY (id);


--
-- Name: task task_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.task
    ADD CONSTRAINT task_pkey PRIMARY KEY (id);


--
-- Name: variable variable_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.variable
    ADD CONSTRAINT variable_pkey PRIMARY KEY (id);


--
-- Name: vendor vendor_name_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.vendor
    ADD CONSTRAINT vendor_name_key UNIQUE (name);


--
-- Name: vendor vendor_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.vendor
    ADD CONSTRAINT vendor_pkey PRIMARY KEY (id);


--
-- Name: alarm_active_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX alarm_active_idx ON public.alarm USING btree (component_id) WHERE (cleared_at IS NULL);


--
-- Name: audit_log_ts_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX audit_log_ts_idx ON public.audit_log USING btree (ts);


--
-- Name: capability_name_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX capability_name_idx ON public.capability USING btree (name);


--
-- Name: component_location_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX component_location_idx ON public.component USING btree (location_id);


--
-- Name: component_parent_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX component_parent_idx ON public.component USING btree (parent_id);


--
-- Name: component_product_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX component_product_idx ON public.component USING btree (product_id);


--
-- Name: credential_one_password; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX credential_one_password ON public.credential USING btree (principal_id) WHERE (kind = 'password'::text);


--
-- Name: credential_secret_hash_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX credential_secret_hash_key ON public.credential USING btree (secret_hash);


--
-- Name: driver_name_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX driver_name_idx ON public.driver USING btree (name);


--
-- Name: event_owner_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX event_owner_idx ON public.event USING btree (component_id, property_id, instance, ts DESC) WHERE (component_id IS NOT NULL);


--
-- Name: event_ts_brin; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX event_ts_brin ON public.event USING brin (ts);


--
-- Name: file_sha256; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX file_sha256 ON public.file USING btree (sha256);


--
-- Name: impersonation_session_active_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX impersonation_session_active_idx ON public.impersonation_session USING btree (expires_at) WHERE (revoked_at IS NULL);


--
-- Name: interface_node_name_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX interface_node_name_idx ON public.interface USING btree (node_name) WHERE (node_name IS NOT NULL);


--
-- Name: interface_type_name_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX interface_type_name_idx ON public.interface_type USING btree (name);


--
-- Name: location_parent_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX location_parent_idx ON public.location USING btree (parent_id);


--
-- Name: location_type_name_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX location_type_name_idx ON public.location_type USING btree (name);


--
-- Name: location_type_property_property_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX location_type_property_property_idx ON public.location_type_property USING btree (property_id);


--
-- Name: metric_owner_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX metric_owner_idx ON public.metric USING btree (component_id, property_id, instance, ts DESC) WHERE (component_id IS NOT NULL);


--
-- Name: metric_ts_brin; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX metric_ts_brin ON public.metric USING brin (ts);


--
-- Name: principal_grant_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX principal_grant_unique ON public.principal_grant USING btree (principal_id, role_id, scope_kind, COALESCE(scope_id, ''::text), scope_op);


--
-- Name: principal_group_grant_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX principal_group_grant_unique ON public.principal_grant USING btree (group_id, role_id, scope_kind, COALESCE(scope_id, ''::text), scope_op) WHERE (group_id IS NOT NULL);


--
-- Name: product_capability_capability_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX product_capability_capability_idx ON public.product_capability USING btree (capability_id);


--
-- Name: product_capability_product_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX product_capability_product_idx ON public.product_capability USING btree (product_id);


--
-- Name: product_driver_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX product_driver_idx ON public.product USING btree (driver_id);


--
-- Name: product_name_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX product_name_idx ON public.product USING btree (name);


--
-- Name: product_parent_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX product_parent_idx ON public.product USING btree (parent_product_id);


--
-- Name: product_property_property_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX product_property_property_idx ON public.product_property USING btree (property_id);


--
-- Name: product_vendor_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX product_vendor_idx ON public.product USING btree (vendor_id);


--
-- Name: property_value_component_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX property_value_component_idx ON public.property_value USING btree (component_id, property_id) WHERE (component_id IS NOT NULL);


--
-- Name: system_role_assignment_component_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX system_role_assignment_component_idx ON public.system_role_assignment USING btree (component_id);


--
-- Name: system_role_assignment_system_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX system_role_assignment_system_idx ON public.system_role_assignment USING btree (system_id);


--
-- Name: secret_component_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX secret_component_idx ON public.secret USING btree (component_id);


--
-- Name: secret_component_key_uuid; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX secret_component_key_uuid ON public.secret USING btree (name, component_id) WHERE (owner_kind = 'component'::text);


--
-- Name: secret_location_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX secret_location_idx ON public.secret USING btree (location_id);


--
-- Name: secret_location_key_uuid; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX secret_location_key_uuid ON public.secret USING btree (name, location_id) WHERE (owner_kind = 'location'::text);


--
-- Name: secret_name_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX secret_name_idx ON public.secret USING btree (name);


--
-- Name: secret_platform_name; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX secret_platform_name ON public.secret USING btree (name) WHERE (owner_kind = 'platform'::text);


--
-- Name: secret_system_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX secret_system_idx ON public.secret USING btree (system_id);


--
-- Name: secret_system_key_uuid; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX secret_system_key_uuid ON public.secret USING btree (name, system_id) WHERE (owner_kind = 'system'::text);


--
-- Name: secret_type_name_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX secret_type_name_idx ON public.secret_type USING btree (name);


--
-- Name: standard_name_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX standard_name_idx ON public.standard USING btree (name);


--
-- Name: standard_property_property_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX standard_property_property_idx ON public.standard_property USING btree (property_id);


--
-- Name: state_owner_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX state_owner_idx ON public.state USING btree (component_id, property_id, instance, ts DESC) WHERE (component_id IS NOT NULL);


--
-- Name: state_ts_brin; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX state_ts_brin ON public.state USING brin (ts);


--
-- Name: system_location_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX system_location_idx ON public.system USING btree (location_id);


--
-- Name: system_member_component_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX system_member_component_idx ON public.system_member USING btree (component_id);


--
-- Name: system_member_one_primary_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX system_member_one_primary_idx ON public.system_member USING btree (component_id) WHERE is_primary;


--
-- Name: system_member_system_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX system_member_system_idx ON public.system_member USING btree (system_id);


--
-- Name: system_parent_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX system_parent_idx ON public.system USING btree (parent_id);


--
-- Name: system_role_standard_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX system_role_standard_idx ON public.system_role USING btree (standard_id) WHERE (standard_id IS NOT NULL);


--
-- Name: system_role_system_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX system_role_system_idx ON public.system_role USING btree (system_id) WHERE (system_id IS NOT NULL);


--
-- Name: tag_binding_component_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX tag_binding_component_idx ON public.tag_binding USING btree (component_id);


--
-- Name: tag_binding_component_key_uuid; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX tag_binding_component_key_uuid ON public.tag_binding USING btree (tag_id, component_id) WHERE (owner_kind = 'component'::text);


--
-- Name: tag_binding_location_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX tag_binding_location_idx ON public.tag_binding USING btree (location_id);


--
-- Name: tag_binding_location_key_uuid; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX tag_binding_location_key_uuid ON public.tag_binding USING btree (tag_id, location_id) WHERE (owner_kind = 'location'::text);


--
-- Name: tag_binding_node_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX tag_binding_node_idx ON public.tag_binding USING btree (node_id);


--
-- Name: tag_binding_node_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX tag_binding_node_key ON public.tag_binding USING btree (tag_id, node_id) WHERE (owner_kind = 'node'::text);


--
-- Name: tag_binding_platform_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX tag_binding_platform_key ON public.tag_binding USING btree (tag_id) WHERE (owner_kind = 'platform'::text);


--
-- Name: tag_binding_system_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX tag_binding_system_idx ON public.tag_binding USING btree (system_id);


--
-- Name: tag_binding_system_key_uuid; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX tag_binding_system_key_uuid ON public.tag_binding USING btree (tag_id, system_id) WHERE (owner_kind = 'system'::text);


--
-- Name: tag_binding_tag_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX tag_binding_tag_idx ON public.tag_binding USING btree (tag_id);


--
-- Name: tag_name_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX tag_name_idx ON public.tag USING btree (name);


--
-- Name: task_interface_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX task_interface_idx ON public.task USING btree (interface_id);


--
-- Name: variable_component_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX variable_component_idx ON public.variable USING btree (component_id);


--
-- Name: variable_component_key_uuid; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX variable_component_key_uuid ON public.variable USING btree (name, component_id) WHERE (owner_kind = 'component'::text);


--
-- Name: variable_location_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX variable_location_idx ON public.variable USING btree (location_id);


--
-- Name: variable_location_key_uuid; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX variable_location_key_uuid ON public.variable USING btree (name, location_id) WHERE (owner_kind = 'location'::text);


--
-- Name: variable_name_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX variable_name_idx ON public.variable USING btree (name);


--
-- Name: variable_platform_name; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX variable_platform_name ON public.variable USING btree (name) WHERE (owner_kind = 'platform'::text);


--
-- Name: variable_system_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX variable_system_idx ON public.variable USING btree (system_id);


--
-- Name: variable_system_key_uuid; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX variable_system_key_uuid ON public.variable USING btree (name, system_id) WHERE (owner_kind = 'system'::text);


--
-- Name: vendor_name_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX vendor_name_idx ON public.vendor USING btree (name);


--
-- Name: principal_grant principal_grant_owner_guard; Type: TRIGGER; Schema: public; Owner: -
--

CREATE CONSTRAINT TRIGGER principal_grant_owner_guard AFTER DELETE OR UPDATE ON public.principal_grant DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION public.assert_owner_grant_exists();


--
-- Name: alarm_capability alarm_capability_alarm_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.alarm_capability
    ADD CONSTRAINT alarm_capability_alarm_id_fkey FOREIGN KEY (alarm_id) REFERENCES public.alarm(id) ON DELETE CASCADE;


--
-- Name: alarm_capability alarm_capability_capability_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.alarm_capability
    ADD CONSTRAINT alarm_capability_capability_id_fkey FOREIGN KEY (capability_id) REFERENCES public.capability(id) ON DELETE CASCADE;


--
-- Name: alarm alarm_component_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.alarm
    ADD CONSTRAINT alarm_component_id_fkey FOREIGN KEY (component_id) REFERENCES public.component(id) ON DELETE CASCADE;


--
-- Name: audit_log audit_log_actor_principal_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.audit_log
    ADD CONSTRAINT audit_log_actor_principal_id_fkey FOREIGN KEY (actor_principal_id) REFERENCES public.principal(id) ON DELETE SET NULL;


--
-- Name: audit_log audit_log_real_actor_principal_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.audit_log
    ADD CONSTRAINT audit_log_real_actor_principal_id_fkey FOREIGN KEY (real_actor_principal_id) REFERENCES public.principal(id) ON DELETE SET NULL;


--
-- Name: component_capability component_capability_capability_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.component_capability
    ADD CONSTRAINT component_capability_capability_id_fkey FOREIGN KEY (capability_id) REFERENCES public.capability(id) ON DELETE CASCADE;


--
-- Name: component_capability component_capability_component_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.component_capability
    ADD CONSTRAINT component_capability_component_id_fkey FOREIGN KEY (component_id) REFERENCES public.component(id) ON DELETE CASCADE;


--
-- Name: component component_location_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.component
    ADD CONSTRAINT component_location_id_fkey FOREIGN KEY (location_id) REFERENCES public.location(id) ON DELETE RESTRICT;


--
-- Name: component component_parent_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.component
    ADD CONSTRAINT component_parent_id_fkey FOREIGN KEY (parent_id) REFERENCES public.component(id) ON DELETE RESTRICT;


--
-- Name: component component_product_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.component
    ADD CONSTRAINT component_product_id_fkey FOREIGN KEY (product_id) REFERENCES public.product(id) ON DELETE RESTRICT;


--
-- Name: credential credential_principal_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.credential
    ADD CONSTRAINT credential_principal_id_fkey FOREIGN KEY (principal_id) REFERENCES public.principal(id) ON DELETE CASCADE;


--
-- Name: event event_component_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.event
    ADD CONSTRAINT event_component_id_fkey FOREIGN KEY (component_id) REFERENCES public.component(id) ON DELETE CASCADE;


--
-- Name: event event_location_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.event
    ADD CONSTRAINT event_location_id_fkey FOREIGN KEY (location_id) REFERENCES public.location(id) ON DELETE CASCADE;


--
-- Name: event event_node_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.event
    ADD CONSTRAINT event_node_id_fkey FOREIGN KEY (node_id) REFERENCES public.node(principal_id) ON DELETE CASCADE;


--
-- Name: event event_property_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.event
    ADD CONSTRAINT event_property_id_fkey FOREIGN KEY (property_id) REFERENCES public.property(id) ON DELETE CASCADE;


--
-- Name: event event_system_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.event
    ADD CONSTRAINT event_system_id_fkey FOREIGN KEY (system_id) REFERENCES public.system(id) ON DELETE CASCADE;


--
-- Name: file file_sha256_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.file
    ADD CONSTRAINT file_sha256_fkey FOREIGN KEY (sha256) REFERENCES public.blob(sha256);


--
-- Name: human human_principal_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.human
    ADD CONSTRAINT human_principal_id_fkey FOREIGN KEY (principal_id) REFERENCES public.principal(id) ON DELETE CASCADE;


--
-- Name: impersonation_session impersonation_session_real_actor_principal_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.impersonation_session
    ADD CONSTRAINT impersonation_session_real_actor_principal_id_fkey FOREIGN KEY (real_actor_principal_id) REFERENCES public.principal(id) ON DELETE CASCADE;


--
-- Name: impersonation_session impersonation_session_target_principal_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.impersonation_session
    ADD CONSTRAINT impersonation_session_target_principal_id_fkey FOREIGN KEY (target_principal_id) REFERENCES public.principal(id) ON DELETE CASCADE;


--
-- Name: interface interface_component_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.interface
    ADD CONSTRAINT interface_component_fkey FOREIGN KEY (component) REFERENCES public.component(id) ON DELETE SET NULL;


--
-- Name: interface interface_node_name_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.interface
    ADD CONSTRAINT interface_node_name_fkey FOREIGN KEY (node_name) REFERENCES public.node(principal_id) ON DELETE CASCADE;


--
-- Name: interface interface_type_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.interface
    ADD CONSTRAINT interface_type_fkey FOREIGN KEY (type) REFERENCES public.interface_type(id);


--
-- Name: location location_location_type_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.location
    ADD CONSTRAINT location_location_type_fkey FOREIGN KEY (location_type) REFERENCES public.location_type(id);


--
-- Name: location location_parent_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.location
    ADD CONSTRAINT location_parent_id_fkey FOREIGN KEY (parent_id) REFERENCES public.location(id) ON DELETE RESTRICT;


--
-- Name: location_type_property location_type_property_location_type_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.location_type_property
    ADD CONSTRAINT location_type_property_location_type_id_fkey FOREIGN KEY (location_type_id) REFERENCES public.location_type(id) ON DELETE CASCADE;


--
-- Name: location_type_property location_type_property_property_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.location_type_property
    ADD CONSTRAINT location_type_property_property_id_fkey FOREIGN KEY (property_id) REFERENCES public.property(id) ON DELETE CASCADE;


--
-- Name: metric metric_component_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.metric
    ADD CONSTRAINT metric_component_id_fkey FOREIGN KEY (component_id) REFERENCES public.component(id) ON DELETE CASCADE;


--
-- Name: metric metric_event_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.metric
    ADD CONSTRAINT metric_event_id_fkey FOREIGN KEY (event_id) REFERENCES public.event(id) ON DELETE SET NULL;


--
-- Name: metric metric_location_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.metric
    ADD CONSTRAINT metric_location_id_fkey FOREIGN KEY (location_id) REFERENCES public.location(id) ON DELETE CASCADE;


--
-- Name: metric metric_node_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.metric
    ADD CONSTRAINT metric_node_id_fkey FOREIGN KEY (node_id) REFERENCES public.node(principal_id) ON DELETE CASCADE;


--
-- Name: metric metric_property_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.metric
    ADD CONSTRAINT metric_property_id_fkey FOREIGN KEY (property_id) REFERENCES public.property(id) ON DELETE CASCADE;


--
-- Name: metric metric_system_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.metric
    ADD CONSTRAINT metric_system_id_fkey FOREIGN KEY (system_id) REFERENCES public.system(id) ON DELETE CASCADE;


--
-- Name: node node_location_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.node
    ADD CONSTRAINT node_location_id_fkey FOREIGN KEY (location_id) REFERENCES public.location(id) ON DELETE SET NULL;


--
-- Name: node node_principal_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.node
    ADD CONSTRAINT node_principal_id_fkey FOREIGN KEY (principal_id) REFERENCES public.principal(id) ON DELETE CASCADE;


--
-- Name: principal_grant principal_grant_group_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.principal_grant
    ADD CONSTRAINT principal_grant_group_id_fkey FOREIGN KEY (group_id) REFERENCES public.principal_group(id) ON DELETE CASCADE;


--
-- Name: principal_grant principal_grant_principal_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.principal_grant
    ADD CONSTRAINT principal_grant_principal_id_fkey FOREIGN KEY (principal_id) REFERENCES public.principal(id) ON DELETE CASCADE;


--
-- Name: principal_grant principal_grant_role_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.principal_grant
    ADD CONSTRAINT principal_grant_role_id_fkey FOREIGN KEY (role_id) REFERENCES public.role(id);


--
-- Name: principal_group_member principal_group_member_group_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.principal_group_member
    ADD CONSTRAINT principal_group_member_group_id_fkey FOREIGN KEY (group_id) REFERENCES public.principal_group(id) ON DELETE CASCADE;


--
-- Name: principal_group_member principal_group_member_principal_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.principal_group_member
    ADD CONSTRAINT principal_group_member_principal_id_fkey FOREIGN KEY (principal_id) REFERENCES public.principal(id) ON DELETE CASCADE;


--
-- Name: product_capability product_capability_capability_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.product_capability
    ADD CONSTRAINT product_capability_capability_id_fkey FOREIGN KEY (capability_id) REFERENCES public.capability(id) ON DELETE CASCADE;


--
-- Name: product_capability product_capability_product_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.product_capability
    ADD CONSTRAINT product_capability_product_id_fkey FOREIGN KEY (product_id) REFERENCES public.product(id) ON DELETE CASCADE;


--
-- Name: product product_driver_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.product
    ADD CONSTRAINT product_driver_id_fkey FOREIGN KEY (driver_id) REFERENCES public.driver(id) ON DELETE SET NULL;


--
-- Name: product product_parent_product_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.product
    ADD CONSTRAINT product_parent_product_id_fkey FOREIGN KEY (parent_product_id) REFERENCES public.product(id) ON DELETE SET NULL;


--
-- Name: product_property product_property_product_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.product_property
    ADD CONSTRAINT product_property_product_id_fkey FOREIGN KEY (product_id) REFERENCES public.product(id) ON DELETE CASCADE;


--
-- Name: product_property product_property_property_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.product_property
    ADD CONSTRAINT product_property_property_id_fkey FOREIGN KEY (property_id) REFERENCES public.property(id) ON DELETE CASCADE;


--
-- Name: product product_vendor_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.product
    ADD CONSTRAINT product_vendor_id_fkey FOREIGN KEY (vendor_id) REFERENCES public.vendor(id) ON DELETE SET NULL;


--
-- Name: property_value property_value_component_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.property_value
    ADD CONSTRAINT property_value_component_id_fkey FOREIGN KEY (component_id) REFERENCES public.component(id) ON DELETE CASCADE;


--
-- Name: property_value property_value_location_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.property_value
    ADD CONSTRAINT property_value_location_id_fkey FOREIGN KEY (location_id) REFERENCES public.location(id) ON DELETE CASCADE;


--
-- Name: property_value property_value_node_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.property_value
    ADD CONSTRAINT property_value_node_id_fkey FOREIGN KEY (node_id) REFERENCES public.node(principal_id) ON DELETE CASCADE;


--
-- Name: property_value property_value_property_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.property_value
    ADD CONSTRAINT property_value_property_id_fkey FOREIGN KEY (property_id) REFERENCES public.property(id) ON DELETE CASCADE;


--
-- Name: property_value property_value_system_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.property_value
    ADD CONSTRAINT property_value_system_id_fkey FOREIGN KEY (system_id) REFERENCES public.system(id) ON DELETE CASCADE;


--
-- Name: system_role_assignment system_role_assignment_component_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.system_role_assignment
    ADD CONSTRAINT system_role_assignment_component_id_fkey FOREIGN KEY (component_id) REFERENCES public.component(id) ON DELETE RESTRICT;


--
-- Name: system_role_assignment system_role_assignment_role_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.system_role_assignment
    ADD CONSTRAINT system_role_assignment_role_id_fkey FOREIGN KEY (role_id) REFERENCES public.system_role(id) ON DELETE CASCADE;


--
-- Name: system_role_assignment system_role_assignment_system_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.system_role_assignment
    ADD CONSTRAINT system_role_assignment_system_id_fkey FOREIGN KEY (system_id) REFERENCES public.system(id) ON DELETE CASCADE;


--
-- Name: system_role_capability system_role_capability_capability_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.system_role_capability
    ADD CONSTRAINT system_role_capability_capability_id_fkey FOREIGN KEY (capability_id) REFERENCES public.capability(id) ON DELETE CASCADE;


--
-- Name: system_role_capability system_role_capability_role_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.system_role_capability
    ADD CONSTRAINT system_role_capability_role_id_fkey FOREIGN KEY (role_id) REFERENCES public.system_role(id) ON DELETE CASCADE;


--
-- Name: secret secret_component_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.secret
    ADD CONSTRAINT secret_component_id_fkey FOREIGN KEY (component_id) REFERENCES public.component(id) ON DELETE CASCADE;


--
-- Name: secret secret_location_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.secret
    ADD CONSTRAINT secret_location_id_fkey FOREIGN KEY (location_id) REFERENCES public.location(id) ON DELETE CASCADE;


--
-- Name: secret secret_secret_type_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.secret
    ADD CONSTRAINT secret_secret_type_fkey FOREIGN KEY (secret_type) REFERENCES public.secret_type(id);


--
-- Name: secret secret_system_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.secret
    ADD CONSTRAINT secret_system_id_fkey FOREIGN KEY (system_id) REFERENCES public.system(id) ON DELETE CASCADE;


--
-- Name: service service_principal_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.service
    ADD CONSTRAINT service_principal_id_fkey FOREIGN KEY (principal_id) REFERENCES public.principal(id) ON DELETE CASCADE;


--
-- Name: standard standard_parent_standard_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.standard
    ADD CONSTRAINT standard_parent_standard_id_fkey FOREIGN KEY (parent_standard_id) REFERENCES public.standard(id) ON DELETE SET NULL;


--
-- Name: standard_property standard_property_property_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.standard_property
    ADD CONSTRAINT standard_property_property_id_fkey FOREIGN KEY (property_id) REFERENCES public.property(id) ON DELETE CASCADE;


--
-- Name: standard_property standard_property_standard_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.standard_property
    ADD CONSTRAINT standard_property_standard_id_fkey FOREIGN KEY (standard_id) REFERENCES public.standard(id) ON DELETE CASCADE;


--
-- Name: state state_component_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.state
    ADD CONSTRAINT state_component_id_fkey FOREIGN KEY (component_id) REFERENCES public.component(id) ON DELETE CASCADE;


--
-- Name: state state_event_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.state
    ADD CONSTRAINT state_event_id_fkey FOREIGN KEY (event_id) REFERENCES public.event(id) ON DELETE SET NULL;


--
-- Name: state state_location_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.state
    ADD CONSTRAINT state_location_id_fkey FOREIGN KEY (location_id) REFERENCES public.location(id) ON DELETE CASCADE;


--
-- Name: state state_node_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.state
    ADD CONSTRAINT state_node_id_fkey FOREIGN KEY (node_id) REFERENCES public.node(principal_id) ON DELETE CASCADE;


--
-- Name: state state_property_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.state
    ADD CONSTRAINT state_property_id_fkey FOREIGN KEY (property_id) REFERENCES public.property(id) ON DELETE CASCADE;


--
-- Name: state state_system_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.state
    ADD CONSTRAINT state_system_id_fkey FOREIGN KEY (system_id) REFERENCES public.system(id) ON DELETE CASCADE;


--
-- Name: system system_location_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.system
    ADD CONSTRAINT system_location_id_fkey FOREIGN KEY (location_id) REFERENCES public.location(id) ON DELETE RESTRICT;


--
-- Name: system_member system_member_component_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.system_member
    ADD CONSTRAINT system_member_component_id_fkey FOREIGN KEY (component_id) REFERENCES public.component(id) ON DELETE CASCADE;


--
-- Name: system_member system_member_system_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.system_member
    ADD CONSTRAINT system_member_system_id_fkey FOREIGN KEY (system_id) REFERENCES public.system(id) ON DELETE CASCADE;


--
-- Name: system system_parent_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.system
    ADD CONSTRAINT system_parent_id_fkey FOREIGN KEY (parent_id) REFERENCES public.system(id) ON DELETE RESTRICT;


--
-- Name: system_role system_role_standard_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.system_role
    ADD CONSTRAINT system_role_standard_id_fkey FOREIGN KEY (standard_id) REFERENCES public.standard(id) ON DELETE CASCADE;


--
-- Name: system_role system_role_system_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.system_role
    ADD CONSTRAINT system_role_system_id_fkey FOREIGN KEY (system_id) REFERENCES public.system(id) ON DELETE CASCADE;


--
-- Name: system system_standard_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.system
    ADD CONSTRAINT system_standard_id_fkey FOREIGN KEY (standard_id) REFERENCES public.standard(id) ON DELETE SET NULL;


--
-- Name: tag_binding tag_binding_component_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tag_binding
    ADD CONSTRAINT tag_binding_component_id_fkey FOREIGN KEY (component_id) REFERENCES public.component(id) ON DELETE CASCADE;


--
-- Name: tag_binding tag_binding_location_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tag_binding
    ADD CONSTRAINT tag_binding_location_id_fkey FOREIGN KEY (location_id) REFERENCES public.location(id) ON DELETE CASCADE;


--
-- Name: tag_binding tag_binding_node_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tag_binding
    ADD CONSTRAINT tag_binding_node_id_fkey FOREIGN KEY (node_id) REFERENCES public.node(principal_id) ON DELETE CASCADE;


--
-- Name: tag_binding tag_binding_system_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tag_binding
    ADD CONSTRAINT tag_binding_system_id_fkey FOREIGN KEY (system_id) REFERENCES public.system(id) ON DELETE CASCADE;


--
-- Name: tag_binding tag_binding_tag_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tag_binding
    ADD CONSTRAINT tag_binding_tag_id_fkey FOREIGN KEY (tag_id) REFERENCES public.tag(id) ON DELETE CASCADE;


--
-- Name: task task_interface_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.task
    ADD CONSTRAINT task_interface_id_fkey FOREIGN KEY (interface_id) REFERENCES public.interface(id) ON DELETE CASCADE;


--
-- Name: variable variable_component_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.variable
    ADD CONSTRAINT variable_component_id_fkey FOREIGN KEY (component_id) REFERENCES public.component(id) ON DELETE CASCADE;


--
-- Name: variable variable_location_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.variable
    ADD CONSTRAINT variable_location_id_fkey FOREIGN KEY (location_id) REFERENCES public.location(id) ON DELETE CASCADE;


--
-- Name: variable variable_system_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.variable
    ADD CONSTRAINT variable_system_id_fkey FOREIGN KEY (system_id) REFERENCES public.system(id) ON DELETE CASCADE;


--
-- PostgreSQL database dump complete
--

-- migrate:down

drop table if exists alarm cascade;
drop table if exists alarm_capability cascade;
drop table if exists audit_log cascade;
drop table if exists blob cascade;
drop table if exists capability cascade;
drop table if exists component cascade;
drop table if exists component_capability cascade;
drop table if exists credential cascade;
drop table if exists driver cascade;
drop table if exists event cascade;
drop table if exists file cascade;
drop table if exists human cascade;
drop table if exists impersonation_session cascade;
drop table if exists interface cascade;
drop table if exists interface_type cascade;
drop table if exists location cascade;
drop table if exists location_type cascade;
drop table if exists location_type_property cascade;
drop table if exists metric cascade;
drop table if exists node cascade;
drop table if exists platform_setting cascade;
drop table if exists principal cascade;
drop table if exists principal_grant cascade;
drop table if exists principal_group cascade;
drop table if exists principal_group_member cascade;
drop table if exists product cascade;
drop table if exists product_capability cascade;
drop table if exists product_property cascade;
drop table if exists property cascade;
drop table if exists property_value cascade;
drop table if exists role cascade;
drop table if exists system_role_assignment cascade;
drop table if exists system_role_capability cascade;
drop table if exists secret cascade;
drop table if exists secret_type cascade;
drop table if exists service cascade;
drop table if exists setting_override cascade;
drop table if exists standard cascade;
drop table if exists standard_property cascade;
drop table if exists state cascade;
drop table if exists system cascade;
drop table if exists system_member cascade;
drop table if exists system_role cascade;
drop table if exists tag cascade;
drop table if exists tag_binding cascade;
drop table if exists task cascade;
drop table if exists variable cascade;
drop table if exists vendor cascade;
drop function if exists assert_owner_grant_exists cascade;
drop function if exists principal_label cascade;
