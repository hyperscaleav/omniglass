import { api } from "../api/client";
import type { components } from "../api/schema.gen";

// The expression data layer (#958, #959, #960): thin typed wrappers over
// POST /expressions:validate (does a source compile at a site, and what may it
// name there) and POST /event-rules:preview (what would a candidate rule have
// done), plus the pure half the expression field renders a diagnostic with.

export type ValidateExpression = components["schemas"]["ValidateExpressionInputBody"];
export type ExpressionCheck = components["schemas"]["ValidateExpressionOutputBody"];
export type ExpressionDiagnostic = components["schemas"]["ExpressionDiagnostic"];
export type ExpressionBinding = components["schemas"]["ExpressionBinding"];
export type PreviewEventRule = components["schemas"]["PreviewEventRuleInputBody"];
export type RulePreview = components["schemas"]["PreviewEventRuleOutputBody"];

export async function validateExpression(body: ValidateExpression): Promise<ExpressionCheck> {
  const { data, error } = await api.POST("/expressions:validate", { body });
  if (error) throw error;
  return data as ExpressionCheck;
}

export async function previewEventRule(body: PreviewEventRule): Promise<RulePreview> {
  const { data, error } = await api.POST("/event-rules:preview", { body });
  if (error) throw error;
  return data as RulePreview;
}

// caretOf is the offending line of a source and a caret under a diagnostic's
// 1-based column, the way a compiler points; null when the diagnostic has no
// position or the line is not in the source.
export function caretOf(source: string, line: number, column: number): { text: string; caret: string } | null {
  if (line < 1 || column < 1) return null;
  const lines = source.split("\n");
  if (line > lines.length) return null;
  return { text: lines[line - 1], caret: `${" ".repeat(column - 1)}^` };
}
