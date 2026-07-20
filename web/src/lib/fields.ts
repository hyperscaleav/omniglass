import { api } from "../api/client";

// The fields data layer: thin typed wrappers over the generated client. A field is
// a typed literal declared on a component_type (a field_definition) and optionally
// set on an individual component (a field_value). Unlike a variable, a field is not
// a scope cascade: it is a per-type schema resolved on one component to the set
// literal or the type-level default. The data_type set matches a variable's value
// type, so the value coercion helpers in lib/variables (displayValue / parseInput)
// are reused rather than duplicated.

export type FieldDataType = "string" | "int" | "float" | "bool" | "json";

export const FIELD_DATA_TYPES: FieldDataType[] = ["string", "int", "float", "bool", "json"];

// FieldDefinition is one field declared on a component_type: it draws its identity
// from a canonical key (name = key, data_type and display_name come from the key),
// plus the per-type schema bits (an optional default and the required flag).
export type FieldDefinition = {
  id: string;
  component_type: string;
  name: string;
  // The canonical key this field declares (== name). Omitted only for a legacy
  // field that predates the registry.
  key?: string;
  display_name?: string;
  data_type: string;
  default_value?: unknown;
  required: boolean;
};

// EffectiveField is one row of a component's effective fields: the declared field,
// its effective value (the set literal or the type default), the override literal
// when set, and is_set marking whether this component overrides the default.
export type EffectiveField = {
  field_id: string;
  name: string;
  display_name?: string;
  data_type: string;
  value: unknown;
  set_value?: unknown;
  // The type-level default (the drill-in's type-default step); omitted when the
  // field definition has no default.
  default_value?: unknown;
  is_set: boolean;
  // Whether the field is required: it must resolve to a value, so it stays
  // overridden and blocks Save while empty.
  required: boolean;
  // The field_value id when set (is_set): the id to delete to clear the override
  // back to the type default. Omitted when the field is unset.
  value_id?: string;
};

export const FIELD_DEFINITIONS_KEY = ["field-definitions"] as const;
export const effectiveFieldsKey = (component: string) => ["effective-fields", component] as const;

export async function listFieldDefinitions(): Promise<FieldDefinition[]> {
  const { data, error } = await api.GET("/field-definitions");
  if (error) throw error;
  return (data?.field_definitions ?? []) as FieldDefinition[];
}

// CreateFieldDefinition declares a field on a component_type by picking a key from
// the catalog. The field's name, data_type, and label come from the key, so only
// the key reference and the per-type schema bits (default, required) are sent.
export type CreateFieldDefinition = {
  component_type: string;
  key: string;
  default_value?: unknown;
  required?: boolean;
};

export async function createFieldDefinition(body: CreateFieldDefinition): Promise<FieldDefinition> {
  const { data, error } = await api.POST("/field-definitions", { body });
  if (error) throw error;
  return data as FieldDefinition;
}

// UpdateFieldDefinition patches a field's per-type schema bits (the default value
// and required flag); the key, data_type, and label are fixed at creation.
export type UpdateFieldDefinition = {
  default_value?: unknown;
  required?: boolean;
};

export async function updateFieldDefinition(id: string, body: UpdateFieldDefinition): Promise<FieldDefinition> {
  const { data, error } = await api.PATCH("/field-definitions/{id}", {
    params: { path: { id } },
    body,
  });
  if (error) throw error;
  return data as FieldDefinition;
}

// deleteFieldDefinition removes a field declared on a component_type by its id.
export async function deleteFieldDefinition(id: string): Promise<void> {
  const { error } = await api.DELETE("/field-definitions/{id}", {
    params: { path: { id } },
  });
  if (error) throw error;
}

// effectiveFields reads a component's effective fields: every field declared on its
// type, resolved to the set literal or the type default.
export async function effectiveFields(component: string): Promise<EffectiveField[]> {
  const { data, error } = await api.GET("/components/{name}/fields", {
    params: { path: { name: component } },
  });
  if (error) throw error;
  return (data?.fields ?? []) as EffectiveField[];
}

// setFieldValue writes a literal for a field on a component (validated server-side
// against the field's data_type). The value is coerced to its data_type by the
// caller so an int field carries a number, not a string.
export async function setFieldValue(component: string, field: string, value: unknown): Promise<void> {
  const { error } = await api.POST("/components/{name}/fields", {
    params: { path: { name: component } },
    body: { field, value },
  });
  if (error) throw error;
}

// deleteFieldValue clears a component's override for a field, reverting it to the
// type-level default. The id is the effective row's value_id (present only when the
// field is set).
export async function deleteFieldValue(id: string): Promise<void> {
  const { error } = await api.DELETE("/field-values/{id}", {
    params: { path: { id } },
  });
  if (error) throw error;
}
