import { z } from "zod";
import { codePointLength, hasControlChars, utf8Length } from "@/shared/lib/credentials";
import { strings } from "@/shared/lib/strings";
import type { Metadata } from "../api/apps";

// Mirrors the `Metadata` schema in api/openapi.yaml. Lengths count Unicode
// code points, like the server; the size limit is UTF-8 bytes of compact JSON.
export const METADATA_MAX_KEYS = 50;
export const METADATA_MAX_KEY_LENGTH = 40;
export const METADATA_MAX_VALUE_LENGTH = 500;
export const METADATA_MAX_BYTES = 8192;

export type MetadataResult = { ok: true; value: Metadata } | { ok: false; message: string };

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

/** Validates the metadata textarea. Empty text (or `null`) means `{}`. */
export function parseMetadataText(text: string): MetadataResult {
  if (text.trim() === "") return { ok: true, value: {} };
  let parsed: unknown;
  try {
    parsed = JSON.parse(text);
  } catch {
    return { ok: false, message: strings.apps.metadataJson };
  }
  if (parsed === null) return { ok: true, value: {} };
  if (!isPlainObject(parsed)) return { ok: false, message: strings.apps.metadataObject };

  const entries = Object.entries(parsed);
  if (entries.length > METADATA_MAX_KEYS) return { ok: false, message: strings.apps.metadataMaxKeys };
  for (const [key, value] of entries) {
    if (key === "") return { ok: false, message: strings.apps.metadataKeyEmpty };
    if (hasControlChars(key)) return { ok: false, message: strings.apps.metadataKeyControl };
    if (codePointLength(key) > METADATA_MAX_KEY_LENGTH) return { ok: false, message: strings.apps.metadataKeyTooLong(key) };
    if (typeof value === "string") {
      if (value.includes("\u0000")) return { ok: false, message: strings.apps.metadataValueNull(key) };
      if (codePointLength(value) > METADATA_MAX_VALUE_LENGTH) {
        return { ok: false, message: strings.apps.metadataValueTooLong(key) };
      }
    } else if (value !== null && typeof value !== "number" && typeof value !== "boolean") {
      return { ok: false, message: strings.apps.metadataValueType(key) };
    }
  }
  if (utf8Length(JSON.stringify(parsed)) > METADATA_MAX_BYTES) {
    return { ok: false, message: strings.apps.metadataTooLarge };
  }
  return { ok: true, value: parsed as Metadata };
}

/** A form field holding metadata as JSON text. */
export const metadataText = z.string().superRefine((text, ctx) => {
  const result = parseMetadataText(text);
  if (!result.ok) ctx.addIssue({ code: "custom", message: result.message });
});

/** Pretty JSON for the editor; empty for no metadata. */
export function metadataToText(metadata: Metadata | undefined): string {
  return metadata && Object.keys(metadata).length > 0 ? JSON.stringify(metadata, null, 2) : "";
}
