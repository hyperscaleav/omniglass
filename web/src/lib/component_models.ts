import { api } from "../api/client";

// The component-models data layer: thin typed wrappers over the generated
// client for the product catalog (a specific make + model, with lifecycle
// timestamps and front/back product photos). A model is addressed by its id
// (a kebab id, the create-only address); official (seed-owned) rows are
// read-only past creation, refused server-side on update/delete. make_id is
// create-only (not patchable): re-pointing a model at a different make is a
// delete + recreate, same invariant as the make picker itself.

export type ComponentModel = {
  id: string;
  display_name: string;
  make_id: string;
  model_number: string;
  official: boolean;
  family?: string;
  released_at?: string;
  eos_at?: string;
  eol_at?: string;
  front_image_id?: string;
  back_image_id?: string;
};

export const COMPONENT_MODELS_KEY = ["component-models"] as const;

export async function listModels(): Promise<ComponentModel[]> {
  const { data, error } = await api.GET("/component-models");
  if (error) throw error;
  return (data?.models ?? []) as ComponentModel[];
}

export type CreateModel = {
  id: string;
  display_name: string;
  make_id: string;
  model_number: string;
  family?: string;
  released_at?: string;
  eos_at?: string;
  eol_at?: string;
  front_image_id?: string;
  back_image_id?: string;
};

export async function createModel(body: CreateModel): Promise<ComponentModel> {
  const { data, error } = await api.POST("/component-models", { body });
  if (error) throw error;
  return data as ComponentModel;
}

export type UpdateModel = {
  display_name?: string;
  model_number?: string;
  family?: string;
  released_at?: string;
  eos_at?: string;
  eol_at?: string;
  front_image_id?: string;
  back_image_id?: string;
};

export async function updateModel(id: string, body: UpdateModel): Promise<ComponentModel> {
  const { data, error } = await api.PATCH("/component-models/{id}", { params: { path: { id } }, body });
  if (error) throw error;
  return data as ComponentModel;
}

export async function deleteModel(id: string): Promise<void> {
  const { error } = await api.DELETE("/component-models/{id}", { params: { path: { id } } });
  if (error) throw error;
}
