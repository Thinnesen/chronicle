import { useQuery } from "@tanstack/react-query";
import { Link, Navigate, useRouteContext } from "@tanstack/react-router";

import { namespaces } from "../api/client";
import "./HomePage.css";

// HomePage opens the only namespace of the user, or lets the user choose one.
export function HomePage() {
  const { session } = useRouteContext({ from: "/app" });
  const { data, isPending, error } = useQuery({ queryKey: ["namespaces"], queryFn: () => namespaces.listNamespaces({}) });
  if (isPending) {
    return <p className="quiet">Loading your namespaces…</p>;
  }
  if (error) {
    return <p role="alert">Could not load your namespaces: {error.message}</p>;
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
      <h1 className="page-title">Choose a namespace</h1>
      <ul className="ns-list">
        {list.map((n) => (
          <li key={n.name}>
            <Link to="/ns/$namespace" params={{ namespace: n.name }}>
              {n.name}
            </Link>
            <span className="quiet">{n.canSubmit ? "You can run workflows" : "You can view runs"}</span>
          </li>
        ))}
      </ul>
    </section>
  );
}
