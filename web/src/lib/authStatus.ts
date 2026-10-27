import { useQuery } from "@tanstack/solid-query";
import { api } from "../api/client";

// The public /auth/status: whether an owner exists, and whether this server is
// a lab (#1195). It does not change while the console is open, so it is read
// once.
export function useAuthStatus() {
  return useQuery(() => ({
    queryKey: ["auth-status"],
    queryFn: async () => {
      const { data, error } = await api.GET("/auth/status");
      if (error || !data) throw new Error("auth status");
      return data;
    },
    staleTime: Infinity,
  }));
}
