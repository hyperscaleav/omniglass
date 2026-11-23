// Generated from api/openapi.json by web/scripts/gen-settings-schema.mjs (make gen). Do not edit by hand.
export const settingsSchema = {
  "agent": {
    "credential_destination": {
      "description": "Policy for an agent's calls that may point a stored credential somewhere (any operation gated on secret:bind)",
      "type": "string",
      "enum": [
        "always",
        "ask",
        "never"
      ]
    },
    "destructive": {
      "description": "Policy for an agent's writes that overwrite or delete (a PATCH, PUT or DELETE)",
      "type": "string",
      "enum": [
        "always",
        "ask",
        "never"
      ]
    },
    "read": {
      "description": "Policy for an agent's reads (a GET, or a POST gated on a read permission)",
      "type": "string",
      "enum": [
        "always",
        "ask",
        "never"
      ]
    },
    "roles": {
      "description": "Per-role overrides, keyed by role name: a class set here applies to the agents of anyone granted the role, when it is stricter",
      "type": "object"
    },
    "tools": {
      "description": "Per-tool overrides, keyed by operation ID (an MCP tool's name): always, ask or never, applied on top of the tool's class when stricter",
      "type": "object"
    },
    "write": {
      "description": "Policy for an agent's writes that create or act (a POST that is not a read)",
      "type": "string",
      "enum": [
        "always",
        "ask",
        "never"
      ]
    }
  },
  "assistant": {
    "base_url": {
      "description": "The server's base URL, /chat/completions appended. An internal server is reached only when the notification settings' egress_allow names it. Moving it from the console or API while a key_secret is bound takes secret:bind on that secret; the settings file and environment are trusted as deployment config",
      "type": "string"
    },
    "conversation_days": {
      "description": "Days a stored omnibot conversation is kept after it last changed",
      "type": "integer",
      "minimum": 1,
      "maximum": 36500,
      "format": "int64"
    },
    "key_secret": {
      "description": "A platform api-key secret, by name, sent as the bearer on every model call; empty calls without a key (a local server). Setting it from the console or API takes secret:bind on it; the settings file and environment are trusted as deployment config. The key never leaves the server",
      "type": "string"
    },
    "model": {
      "description": "The model's name on that server (openai/gpt-4.1-mini on OpenRouter, llama3.1 on Ollama)",
      "type": "string"
    },
    "provider": {
      "description": "The model omnibot runs on: openai (any OpenAI-compatible chat completions server, OpenRouter by default) or none, which leaves the server's recording, if any",
      "type": "string",
      "enum": [
        "none",
        "openai"
      ]
    }
  },
  "collection": {
    "report_seconds": {
      "description": "Seconds each endpoint's collection telemetry covers: its polls' mean duration and failures, and the pushes its listeners claimed or did not, reported once a window",
      "type": "integer",
      "minimum": 10,
      "maximum": 3600,
      "format": "int64"
    }
  },
  "database": {
    "slow_query_ms": {
      "description": "Milliseconds a query may run before the server logs it as slow (its statement's shape and duration, never its values) and counts it; 0 turns the log off",
      "type": "integer",
      "minimum": 0,
      "maximum": 600000,
      "format": "int64"
    }
  },
  "heartbeat": {
    "interval_seconds": {
      "description": "Seconds between heartbeats",
      "type": "integer",
      "minimum": 10,
      "maximum": 86400,
      "format": "int64"
    },
    "target": {
      "description": "The webhook notification target the server sends its heartbeat to, by name; empty turns the heartbeat off. The request is signed by the target's bound hmac-key secret and subject to the egress allow list",
      "type": "string"
    }
  },
  "keybindings": {
    "close_blade": {
      "description": "Close the top blade",
      "type": "string"
    },
    "command_palette": {
      "description": "Open the command palette",
      "type": "string"
    },
    "open_detail": {
      "description": "Open the detail blade",
      "type": "string"
    },
    "open_edit": {
      "description": "Open the edit pane",
      "type": "string"
    }
  },
  "label": {
    "acronyms": {
      "description": "Words a generated label title-cases to a fixed form, whole-word and case-insensitive",
      "type": "array"
    }
  },
  "notification": {
    "console_url": {
      "description": "The console's base URL as a receiver of a notification reaches it (https://omniglass.example.com); the prefix of the Link a notification's subject and text read. Empty leaves Link empty",
      "type": "string"
    },
    "egress_allow": {
      "description": "The internal destinations a notification may reach, each a CIDR (10.20.0.0/16), an address, or a host name (chat.corp.example). Everything private, loopback or link-local is refused unless named here; a cloud metadata address is refused even when it is; plain http is admitted only to a destination named here",
      "type": "array"
    },
    "mail_from": {
      "description": "The From address of an email notification (Omniglass <alerts@example.com>)",
      "type": "string"
    },
    "mail_relay": {
      "description": "The SMTP relay email notifications go through, host:port (smtp.example.com:587). Empty turns email off. An internal relay is reached only when egress_allow names it",
      "type": "string"
    },
    "mail_secret": {
      "description": "A platform basic-auth secret, by name, the relay session authenticates with; empty sends without authenticating. Setting it from the console or API, or moving mail_relay there while one is set, takes secret:bind on that secret; the settings file and environment are trusted as deployment config",
      "type": "string"
    },
    "mail_security": {
      "description": "How the session to the relay is protected: starttls (the relay must offer it), tls (implicit TLS, port 465), or none, admitted only for a relay egress_allow names (a lab's mail sim)",
      "type": "string",
      "enum": [
        "starttls",
        "tls",
        "none"
      ]
    }
  },
  "retention": {
    "log_debug_days": {
      "description": "Days a debug log line is kept",
      "type": "integer",
      "minimum": 1,
      "maximum": 36500,
      "format": "int64"
    },
    "log_error_days": {
      "description": "Days an error, critical, alert or emergency log line is kept",
      "type": "integer",
      "minimum": 1,
      "maximum": 36500,
      "format": "int64"
    },
    "log_info_days": {
      "description": "Days an info or notice log line, or one with no severity, is kept",
      "type": "integer",
      "minimum": 1,
      "maximum": 36500,
      "format": "int64"
    },
    "log_warning_days": {
      "description": "Days a warning log line is kept",
      "type": "integer",
      "minimum": 1,
      "maximum": 36500,
      "format": "int64"
    },
    "metric_days": {
      "description": "Days a metric sample is kept, unless its type keeps its own (a metric type's retention_days); a sample on the lane's window leaves with its UTC day, up to a day later",
      "type": "integer",
      "minimum": 1,
      "maximum": 36500,
      "format": "int64"
    },
    "outpost_log_debug_days": {
      "description": "Days an outpost's own debug log line is kept; unset follows log_debug_days",
      "type": "integer",
      "minimum": 1,
      "maximum": 36500,
      "format": "int64"
    },
    "outpost_log_error_days": {
      "description": "Days an outpost's own error, critical, alert or emergency log line is kept; unset follows log_error_days",
      "type": "integer",
      "minimum": 1,
      "maximum": 36500,
      "format": "int64"
    },
    "outpost_log_info_days": {
      "description": "Days an outpost's own info or notice log line, or one with no severity, is kept; unset follows log_info_days",
      "type": "integer",
      "minimum": 1,
      "maximum": 36500,
      "format": "int64"
    },
    "outpost_log_warning_days": {
      "description": "Days an outpost's own warning log line is kept; unset follows log_warning_days",
      "type": "integer",
      "minimum": 1,
      "maximum": 36500,
      "format": "int64"
    },
    "property_days": {
      "description": "Days a property transition is kept, unless its type keeps its own (a property type's retention_days)",
      "type": "integer",
      "minimum": 1,
      "maximum": 36500,
      "format": "int64"
    },
    "trend_days": {
      "description": "Days an hourly metric trend is kept (#1276)",
      "type": "integer",
      "minimum": 1,
      "maximum": 36500,
      "format": "int64"
    }
  },
  "sso": {
    "client_secret": {
      "description": "A platform oauth2-client secret, by name: its client_id field is the client id Omniglass is registered as, its client_secret field authenticates the token exchange. Setting it from the console or API takes secret:bind on it; the settings file and environment are trusted as deployment config",
      "type": "string"
    },
    "groups_claim": {
      "description": "The id_token claim carrying the person's groups at the provider (read by group mapping, #2363)",
      "type": "string"
    },
    "issuer": {
      "description": "The identity provider's issuer URL exactly as its id_tokens carry it (https://login.example.com/realms/corp). Empty turns single sign-on off. An on-premises provider is reached only when the notification settings' egress_allow names it. Moving it from the console or API while client_secret is bound takes secret:bind on that secret; the settings file and environment are trusted as deployment config",
      "type": "string"
    },
    "recheck_grace_minutes": {
      "description": "Minutes a link may go without a refresh the provider accepted (an outage) before its sessions and tokens are refused until one is; also the length of a session through a provider that issues no refresh token",
      "type": "integer",
      "minimum": 1,
      "maximum": 10080,
      "format": "int64"
    },
    "recheck_minutes": {
      "description": "Minutes between re-checks: each link with a live session or token through it redeems its refresh token at the provider, and a refusal revokes them",
      "type": "integer",
      "minimum": 1,
      "maximum": 1440,
      "format": "int64"
    },
    "scopes": {
      "description": "The scopes a sign-in asks for, space-separated. offline_access asks for the refresh token the re-check redeems; a provider that issues none gives sessions of recheck_grace_minutes and no API tokens",
      "type": "string"
    }
  },
  "ui": {
    "default_landing": {
      "description": "Route the console opens to (an absolute path)",
      "type": "string",
      "pattern": "^/"
    },
    "theme": {
      "description": "Console color theme",
      "type": "string",
      "enum": [
        "omniglass-dark",
        "omniglass-light"
      ]
    }
  }
} as const;
