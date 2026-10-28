import type { components } from "./schema";

type ServerErrorCode = components["schemas"]["ErrorCode"];
export type FieldError = components["schemas"]["FieldError"];

/** Codes the client adds when there is no usable error envelope. */
export type ClientErrorCode = "network_error" | "unknown_error";
export type ApiErrorCode = ServerErrorCode | ClientErrorCode;

export interface ApiErrorInit {
  status: number;
  code: ApiErrorCode;
  message: string;
  details?: Record<string, unknown>;
}

export class ApiError extends Error {
  readonly status: number;
  readonly code: ApiErrorCode;
  readonly details: Record<string, unknown>;

  constructor({ status, code, message, details = {} }: ApiErrorInit) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
    this.details = details;
  }

  /** Field errors from `details.fields` on `invalid_request`. */
  get fields(): FieldError[] {
    const fields = this.details.fields;
    if (!Array.isArray(fields)) return [];
    return fields.filter(
      (f): f is FieldError =>
        typeof f === "object" && f !== null && typeof f.field === "string" && typeof f.rule === "string",
    );
  }
}

export function isApiError(error: unknown): error is ApiError {
  return error instanceof ApiError;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

/** Turns a parsed error body (or raw text) into an ApiError. */
export function errorFromBody(status: number, body: unknown): ApiError {
  const envelope = isRecord(body) ? body.error : undefined;
  if (isRecord(envelope) && typeof envelope.code === "string" && typeof envelope.message === "string") {
    return new ApiError({
      status,
      code: envelope.code as ApiErrorCode,
      message: envelope.message,
      details: isRecord(envelope.details) ? envelope.details : {},
    });
  }
  return unknownError(status);
}

export function networkError(cause?: unknown): ApiError {
  const error = new ApiError({ status: 0, code: "network_error", message: "network error" });
  if (cause !== undefined) Object.defineProperty(error, "cause", { value: cause });
  return error;
}

export function unknownError(status: number): ApiError {
  return new ApiError({ status, code: "unknown_error", message: "unexpected response" });
}
