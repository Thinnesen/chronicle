// basePath is the path of chronicle on its host, with a slash at the end: "/" or, behind a proxy, for example
// "/chronicle/". The server writes it into the <base href> of the page.
export const basePath = new URL(document.baseURI).pathname;

// at returns the URL path of a server path such as "auth/me".
export function at(path: string): string {
  return basePath + path.replace(/^\//, "");
}
