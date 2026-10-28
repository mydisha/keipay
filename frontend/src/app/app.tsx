import { useEffect, useState } from "react";
import { createBrowserRouter, RouterProvider } from "react-router";
import { createQueryClient } from "@/shared/api/query-client";
import { SESSION_EXPIRED_PATH, setSessionExpiredHandler } from "@/shared/api/session";
import { Providers } from "./providers";
import { routes } from "./routes";

export function App() {
  const [queryClient] = useState(createQueryClient);
  const [router] = useState(() => createBrowserRouter(routes));

  useEffect(() => {
    setSessionExpiredHandler(() => {
      queryClient.clear();
      void router.navigate(SESSION_EXPIRED_PATH, { replace: true });
    });
    return () => setSessionExpiredHandler(null);
  }, [queryClient, router]);

  return (
    <Providers queryClient={queryClient}>
      <RouterProvider router={router} />
    </Providers>
  );
}
