import type { JsonSchema } from "./types";

export function schemaDefaults(schema: JsonSchema): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const [key, p] of Object.entries(schema.properties ?? {})) {
    if (p.default !== undefined) out[key] = p.default;
  }
  return out;
}
