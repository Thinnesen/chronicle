import { createClient, type Interceptor } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";

import { LogService } from "../gen/chronicle/v1/log_pb";
import { NamespaceService } from "../gen/chronicle/v1/namespace_pb";
import { CronService, TemplateService } from "../gen/chronicle/v1/template_pb";
import { WorkflowService } from "../gen/chronicle/v1/workflow_pb";
import { csrfToken } from "./session";

// csrf adds the CSRF token of the session to each call.
const csrf: Interceptor = (next) => (req) => {
  const token = csrfToken();
  if (token) {
    req.header.set("X-CSRF-Token", token);
  }
  return next(req);
};

const transport = createConnectTransport({
  baseUrl: window.location.origin,
  interceptors: [csrf],
  fetch: (input, init) => fetch(input, { ...init, credentials: "same-origin" }),
});

export const workflows = createClient(WorkflowService, transport);
export const namespaces = createClient(NamespaceService, transport);
export const logs = createClient(LogService, transport);
export const templates = createClient(TemplateService, transport);
export const crons = createClient(CronService, transport);
