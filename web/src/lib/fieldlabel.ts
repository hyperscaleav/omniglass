import { createContext, useContext } from "solid-js";

// The id of the visible label of the field a control sits in, supplied by
// FieldRow. A control whose accessible name is built from an aria-labelledby of
// its own (a Kobalte Select trigger names itself by its selected value, and an
// aria-labelledby beats a <label for>) reads it here and adds it, so the field's
// name is "Location" plus the value, not the value alone.
const FieldLabelContext = createContext<string | undefined>(undefined);

export const FieldLabelProvider = FieldLabelContext.Provider;
export const useFieldLabelId = (): string | undefined => useContext(FieldLabelContext);
