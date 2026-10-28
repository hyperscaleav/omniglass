// Generated from api/openapi.json by web/scripts/gen-settings-schema.mjs (make gen). Do not edit by hand.
export const settingsSchema = {
  "keybindings": {
    "close_blade": {
      "type": "string"
    },
    "command_palette": {
      "type": "string"
    },
    "open_detail": {
      "type": "string"
    },
    "open_edit": {
      "type": "string"
    }
  },
  "label": {
    "acronyms": {
      "type": "array"
    }
  },
  "notification": {
    "console_url": {
      "type": "string"
    },
    "egress_allow": {
      "type": "array"
    },
    "mail_from": {
      "type": "string"
    },
    "mail_relay": {
      "type": "string"
    },
    "mail_secret": {
      "type": "string"
    },
    "mail_security": {
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
      "type": "integer",
      "minimum": 1,
      "maximum": 36500,
      "format": "int64"
    },
    "log_error_days": {
      "type": "integer",
      "minimum": 1,
      "maximum": 36500,
      "format": "int64"
    },
    "log_info_days": {
      "type": "integer",
      "minimum": 1,
      "maximum": 36500,
      "format": "int64"
    },
    "log_warning_days": {
      "type": "integer",
      "minimum": 1,
      "maximum": 36500,
      "format": "int64"
    },
    "metric_days": {
      "type": "integer",
      "minimum": 1,
      "maximum": 36500,
      "format": "int64"
    },
    "property_days": {
      "type": "integer",
      "minimum": 1,
      "maximum": 36500,
      "format": "int64"
    },
    "trend_days": {
      "type": "integer",
      "minimum": 1,
      "maximum": 36500,
      "format": "int64"
    }
  },
  "ui": {
    "default_landing": {
      "type": "string",
      "pattern": "^/"
    },
    "theme": {
      "type": "string",
      "enum": [
        "omniglass-dark",
        "omniglass-light"
      ]
    }
  }
} as const;
