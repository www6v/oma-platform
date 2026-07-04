import { createContext, useContext, type ReactNode } from "react";
import { authClient } from "./auth-client";

interface User {
  id: string;
  name: string;
  email: string;
  image?: string | null;
}

interface AuthCtx {
  user: User | null;
  isLoading: boolean;
  isAuthenticated: boolean;
}

const AuthContext = createContext<AuthCtx>({
  user: null,
  isLoading: true,
  isAuthenticated: false,
});

export function AuthProvider({ children }: { children: ReactNode }) {
  const { data: session, isPending } = authClient.useSession();

  // When AUTH is enabled, require a real session; no dev stub user.
  const DEV_BYPASS = false;

  const user = DEV_BYPASS
    ? {
        id: "dev-user",
        name: "Dev User",
        email: "dev@localhost",
        image: null,
      }
    : session?.user
      ? {
          id: session.user.id,
          name: session.user.name,
          email: session.user.email,
          image: session.user.image,
        }
      : null;

  return (
    <AuthContext.Provider
      value={{
        user,
        isLoading: DEV_BYPASS ? false : isPending && !user,
        isAuthenticated: DEV_BYPASS ? true : !!user,
      }}
    >
      {children}
    </AuthContext.Provider>
  );
}

export const useAuth = () => useContext(AuthContext);
