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
  "retention": {
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
