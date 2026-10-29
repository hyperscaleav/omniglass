// Generated from api/openapi.json by web/scripts/gen-settings-schema.mjs (make gen). Do not edit by hand.
export const settingsSchema = {
  "collection": {
    "report_seconds": {
      "description": "Seconds each endpoint's collection telemetry covers: its polls' mean duration and failures, and the pushes its listeners claimed or did not, reported once a window",
      "type": "integer",
      "minimum": 10,
      "maximum": 3600,
      "format": "int64"
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
      "description": "The console's base URL as a receiver of a notification reaches it (https://omniglass.example.com); the prefix of the Link a template reads. Empty leaves Link empty",
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
      "description": "A platform basic-auth secret, by name, the relay session authenticates with; empty sends without authenticating. Setting it, or moving mail_relay while one is set, takes the right to reveal that secret",
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
      "description": "Days a metric sample is kept, unless its type keeps its own (a metric type's retention_days)",
      "type": "integer",
      "minimum": 1,
      "maximum": 36500,
      "format": "int64"
    },
    "node_log_debug_days": {
      "description": "Days a node's own debug log line is kept; unset follows log_debug_days",
      "type": "integer",
      "minimum": 1,
      "maximum": 36500,
      "format": "int64"
    },
    "node_log_error_days": {
      "description": "Days a node's own error, critical, alert or emergency log line is kept; unset follows log_error_days",
      "type": "integer",
      "minimum": 1,
      "maximum": 36500,
      "format": "int64"
    },
    "node_log_info_days": {
      "description": "Days a node's own info or notice log line, or one with no severity, is kept; unset follows log_info_days",
      "type": "integer",
      "minimum": 1,
      "maximum": 36500,
      "format": "int64"
    },
    "node_log_warning_days": {
      "description": "Days a node's own warning log line is kept; unset follows log_warning_days",
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
      "description": "Days an hourly metric trend is kept, once trends exist (#1276)",
      "type": "integer",
      "minimum": 1,
      "maximum": 36500,
      "format": "int64"
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
