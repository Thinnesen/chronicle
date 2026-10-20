import { useQuery } from "@tanstack/react-query";
import { Link, Navigate, useRouteContext } from "@tanstack/react-router";

import { namespaces } from "../api/client";
import "./HomePage.css";
import { useTitle } from "../lib/useTitle";
import { errorText } from "../lib/errors";

// HomePage opens the only namespace of the user, or lets the user choose one.
export function HomePage() {
  useTitle("Namespaces");
  const { session } = useRouteContext({ from: "/app" });
  const { data, isPending, error } = useQuery({ queryKey: ["namespaces"], queryFn: () => namespaces.listNamespaces({}) });
  if (isPending) {
    return <p className="quiet">Loading your namespaces…</p>;
  }
  if (error) {
    return <p role="alert">Could not load your namespaces: {errorText(error)}</p>;
  }
  const list = data.namespaces;
  if (list.length === 1 && list[0]) {
    return <Navigate to="/ns/$namespace" params={{ namespace: list[0].name }} replace />;
  }
  if (list.length === 0) {
    return (
      <section className="home-empty">
        <h1>No namespace yet</h1>
        <p>
          You are logged in as <strong>{session.user}</strong>, but you cannot see runs in any namespace. Ask your
          platform team to add one of your groups to a team.
        </p>
      </section>
    );
  }
  return (
    <section>
      <header className="page-head">
        <div>
          <h1>Choose a namespace</h1>
          <p className="quiet">You can see the runs of these namespaces.</p>
        </div>
        <Link className="button" to="/runs">
          All namespaces
        </Link>
      </header>
      <ul className="item-list">
        {list.map((n) => (
          <li key={n.name}>
            <span className="item-title">
              <Link to="/ns/$namespace" params={{ namespace: n.name }}>
                {n.name}
              </Link>
            </span>
            <span className="item-side">{n.canSubmit ? "You can run workflows" : "You can view runs"}</span>
          </li>
        ))}
      </ul>
    </section>
  );
}
