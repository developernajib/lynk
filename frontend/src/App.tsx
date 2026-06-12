import { useState } from "react";

import type { User } from "./gen/identity/v1/identity_pb";
import Login from "./pages/Login";
import Notes from "./pages/Notes";
import { accessTokenID, clearTokens, currentRefreshToken, identity } from "./transport";

export default function App() {
  const [user, setUser] = useState<User | null>(null);

  async function logout() {
    try {
      await identity.logout({
        refreshToken: currentRefreshToken(),
        accessTokenId: accessTokenID(),
      });
    } finally {
      clearTokens();
      setUser(null);
    }
  }

  if (!user) {
    return <Login onLogin={setUser} />;
  }

  return (
    <div className="shell">
      <header>
        <strong>lynk</strong>
        <span>
          {user.fullName} ({user.email})
        </span>
        <button onClick={logout}>Log out</button>
      </header>
      <Notes />
    </div>
  );
}
