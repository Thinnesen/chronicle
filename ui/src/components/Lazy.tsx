import { lazy, Suspense, type ComponentProps } from "react";

// The YAML parts load on first use, because the yaml package is a large part of the bundle.
const YamlViewPart = lazy(() => import("./YamlView").then((m) => ({ default: m.YamlView })));
const SubmitDialogPart = lazy(() => import("./SubmitDialog").then((m) => ({ default: m.SubmitDialog })));

// YamlView shows an object as YAML. See YamlView.tsx.
export function YamlView(props: ComponentProps<typeof YamlViewPart>) {
  return (
    <Suspense fallback={<pre className="yaml" aria-busy="true" />}>
      <YamlViewPart {...props} />
    </Suspense>
  );
}

// SubmitDialog starts a run from YAML. See SubmitDialog.tsx.
export function SubmitDialog(props: ComponentProps<typeof SubmitDialogPart>) {
  return (
    <Suspense fallback={null}>
      <SubmitDialogPart {...props} />
    </Suspense>
  );
}
