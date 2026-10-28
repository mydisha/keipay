import { useEffect, useState } from "react";
import { createBrowserRouter, RouterProvider } from "react-router";
import { createQueryClient } from "@/shared/api/query-client";
import { setAuthRedirectHandler } from "@/shared/api/session";
import { createAuthRedirectHandler } from "./auth-redirect";
import { Providers } from "./providers";
import { routes } from "./routes";

export function App() {
  const [queryClient] = useState(createQueryClient);
  const [router] = useState(() => createBrowserRouter(routes));

  useEffect(() => {
    setAuthRedirectHandler(createAuthRedirectHandler(queryClient, router));
    return () => setAuthRedirectHandler(null);
  }, [queryClient, router]);

  return (
    <Providers queryClient={queryClient}>
      <RouterProvider router={router} />
    </Providers>
  );
}
