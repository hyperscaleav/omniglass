import { api } from "../api/client";

// The variables data layer: thin typed wrappers over the generated client. A
// variable is a typed, plaintext free value (a macro) owned on the exclusive arc
// (platform, or one of the location / system / component trees) and resolved down
// the cascade. Unlike a secret, the value is shown in the clear.

export type ValueType = "string" | "int" | "float" | "bool" | "json";

export const VALUE_TYPES: ValueType[] = ["string", "int", "float", "bool", "json"];

export type Variable = {
  id: string;
  name: string;
  value_type: string;
  owner_kind: string;

  owner_name?: string;
  value: unknown;
};

export const VARIABLES_KEY = ["variables"] as const;

export type OwnerKind = "platform" | "location" | "system" | "component";

export type CreateVariable = {
  name: string;
  value_type: ValueType;
  owner_kind: OwnerKind;
  owner?: string;
  value: unknown;
};

export async function listVariables(): Promise<Variable[]> {
  const { data, error } = await api.GET("/variables");
  if (error) throw error;
  return (data?.variables ?? []) as Variable[];
}

export async function createVariable(body: CreateVariable): Promise<Variable> {
  const { data, error } = await api.POST("/variables", { body });
  if (error) throw error;
  return data as Variable;
}

// updateVariable replaces the value (validated against the fixed value_type);
// name, type, and owner are fixed at creation.
export async function updateVariable(id: string, value: unknown): Promise<Variable> {
  const { data, error } = await api.PATCH("/variables/{id}", { params: { path: { id } }, body: { value } });
  if (error) throw error;
  return data as Variable;
}

export async function deleteVariable(id: string): Promise<void> {
  const { error } = await api.DELETE("/variables/{id}", { params: { path: { id } } });
  if (error) throw error;
}

// displayValue renders a variable's polymorphic value as a compact string for a
// table cell or an input seed: a string as-is, a scalar via String(), an
// object/array as compact JSON.
export function displayValue(value: unknown): string {
  if (value === null || value === undefined) return "";
  if (typeof value === "string") return value;
  if (typeof value === "object") return JSON.stringify(value);
  return String(value);
}

// parseInput turns a text input back into the typed value the API expects for a
// value_type: an int/float to a number, a bool to true/false, json parsed, a
// string left as text. A number or json that will not parse throws, so the caller
// surfaces the error instead of sending a malformed value.
export function parseInput(valueType: ValueType, text: string): unknown {
  switch (valueType) {
    case "int":
    case "float": {
      const n = Number(text);
      if (text.trim() === "" || Number.isNaN(n)) throw new Error(`not a ${valueType}: ${text}`);
      return n;
    }
    case "bool":
      return text === "true";
    case "json":
      return JSON.parse(text);
    default:
      return text;
  }
}
