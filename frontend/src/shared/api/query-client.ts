import { QueryClient } from "@tanstack/react-query";
import { isApiError } from "./errors";

const MAX_RETRIES = 2;

function shouldRetry(failureCount: number, error: unknown): boolean {
  if (failureCount >= MAX_RETRIES) return false;
  // Client errors won't change on retry; network blips and 5xx might.
  if (isApiError(error) && error.status >= 400 && error.status < 500) return false;
  return true;
}

export function createQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: shouldRetry, staleTime: 15_000, refetchOnWindowFocus: false },
      mutations: { retry: false },
    },
  });
}
