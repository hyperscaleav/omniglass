import {
  classifierProperties,
  classifierPropertiesKey,
  deleteClassifierProperty,
  setClassifierProperty,
  type ClassifierProperty,
  type SetClassifierProperty,
} from "./classifier_properties";

// The product arc of the classifier-contract layer, in product language: a
// product declares what every component that is an instance of it exposes. The
// logic is classifier-generic (lib/classifier_properties), so the product,
// standard, and location-type contracts cannot drift; these wrappers keep the
// product call sites reading as what they address.

export type ProductProperty = ClassifierProperty;
export type SetProductProperty = SetClassifierProperty;

export const productPropertiesKey = (id: string) => classifierPropertiesKey("product", id);

export const productProperties = (id: string): Promise<ProductProperty[]> => classifierProperties("product", id);

export const setProductProperty = (id: string, property: string, body: SetProductProperty): Promise<ProductProperty> =>
  setClassifierProperty("product", id, property, body);

export const deleteProductProperty = (id: string, property: string): Promise<void> =>
  deleteClassifierProperty("product", id, property);
