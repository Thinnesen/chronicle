import { Link } from "@tanstack/react-router";

import { BrandMark } from "../components/BrandMark";
import { useTitle } from "../lib/useTitle";
import "./LoginPage.css";

// NotFoundPage answers a path that the UI does not have, for example an old or mistyped link.
export function NotFoundPage() {
  useTitle("Page not found");
  return (
    <main className="login">
      <div className="login-panel card">
        <BrandMark size={40} />
        <h1>This page does not exist</h1>
        <p>The link may be old or mistyped.</p>
        <Link className="button button-primary login-button" to="/">
          Go to the start page
        </Link>
      </div>
    </main>
  );
}
