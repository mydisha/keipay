import { QueryClient } from "@tanstack/react-query";
import { render } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { createMemoryRouter, RouterProvider } from "react-router";
import { createAuthRedirectHandler } from "@/app/auth-redirect";
import { Providers } from "@/app/providers";
import { routes } from "@/app/routes";
import { setAuthRedirectHandler } from "@/shared/api/session";

export function testQueryClient() {
  return new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity }, mutations: { retry: false } },
  });
}

/** Renders a component inside the app providers and a router at `path`. */
export function renderWithProviders(ui: ReactNode, { path = "/" }: { path?: string } = {}) {
  const queryClient = testQueryClient();
  const router = createMemoryRouter([{ path: "*", element: ui }], { initialEntries: [path] });
  const user = userEvent.setup();
  const result = render(
    <Providers queryClient={queryClient}>
      <RouterProvider router={router} />
    </Providers>,
  );
  return { ...result, user, router, queryClient };
}

/** Renders the whole console at `path`, with the real routes. */
export function renderApp(path: string) {
  const queryClient = testQueryClient();
  const router = createMemoryRouter(routes, { initialEntries: [path] });
  setAuthRedirectHandler(createAuthRedirectHandler(queryClient, router));
  const user = userEvent.setup();
  const result = render(
    <Providers queryClient={queryClient}>
      <RouterProvider router={router} />
    </Providers>,
  );
  return { ...result, user, router, queryClient };
}
