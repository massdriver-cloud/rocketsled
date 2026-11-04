import type { JsonSchema } from "../types";

type Props = {
  schema: JsonSchema;
  order?: string[];
  value: Record<string, unknown>;
  onChange: (next: Record<string, unknown>) => void;
  errorField?: string;
  errorMessage?: string;
};

function orderedKeys(schema: JsonSchema, order?: string[]): string[] {
  const keys = Object.keys(schema.properties ?? {});
  if (!order || order.length === 0) return keys;
  const explicit = order.filter((k) => k !== "*" && keys.includes(k));
  const rest = keys.filter((k) => !explicit.includes(k));
  return [...explicit, ...rest];
}

// Renders a flat JSON Schema (what platform credential types use) as form fields.
export function SchemaForm({ schema, order, value, onChange, errorField, errorMessage }: Props) {
  const required = new Set(schema.required ?? []);
  const props = schema.properties ?? {};
  return (
    <>
      {orderedKeys(schema, order).map((key) => {
        const p = props[key];
        const id = `f-${key}`;
        const hasError = errorField === key;
        const title = p.title ?? key;
        const set = (v: unknown) => onChange({ ...value, [key]: v });
        const options = p.oneOf?.map((o) => ({ value: o.const, label: o.title })) ?? p.enum?.map((e) => ({ value: e, label: String(e) }));
        const numeric = p.type === "integer" || p.type === "number";
        return (
          <div key={key} className={`field ${hasError ? "field--error" : ""}`}>
            <label htmlFor={id}>
              {title}
              {!required.has(key) && <span className="muted"> (optional)</span>}
            </label>
            {p.type === "boolean" ? (
              <label className="check">
                <input id={id} type="checkbox" checked={Boolean(value[key])} onChange={(e) => set(e.target.checked)} />
                {p.description}
              </label>
            ) : options ? (
              <select id={id} value={String(value[key] ?? "")} onChange={(e) => set(e.target.value)}>
                <option value="">Choose…</option>
                {options.map((o) => (
                  <option key={String(o.value)} value={String(o.value)}>
                    {o.label}
                  </option>
                ))}
              </select>
            ) : (
              <input
                id={id}
                type={numeric ? "number" : "text"}
                className={key.includes("arn") || key.includes("id") ? "mono" : undefined}
                value={String(value[key] ?? "")}
                placeholder={p.examples?.[0] ? String(p.examples[0]) : undefined}
                onChange={(e) => set(numeric ? Number(e.target.value) : e.target.value)}
                autoComplete="off"
              />
            )}
            {p.type !== "boolean" && p.description && <span className="hint">{p.description}</span>}
            {hasError && <span className="field__error">{errorMessage}</span>}
          </div>
        );
      })}
    </>
  );
}
