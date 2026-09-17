import { useEffect, useState } from 'react';
import type { AuthMethods } from '../api/types';

/** useAuthMethods drives the "Sign in with SSO" button on the login page,
 * matching both previous pages' `fetch('/auth/methods')` check. */
export function useAuthMethods(): AuthMethods {
  const [methods, setMethods] = useState<AuthMethods>({ oidc: false });

  useEffect(() => {
    fetch('/auth/methods')
      .then((res) => res.json())
      .then((info: AuthMethods) => setMethods(info))
      .catch(() => {});
  }, []);

  return methods;
}
