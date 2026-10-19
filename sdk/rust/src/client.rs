//! The client for the chronicle API server. It uses the Connect protocol with JSON.

use std::fmt;
use std::thread;
use std::time::{Duration, Instant};

use serde_json::{Value, json};

use crate::Workflow;

/// An error answer of the server, with its Connect code (for example "permission_denied"), or a transport error.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ApiError {
    pub code: String,
    pub message: String,
}

impl fmt::Display for ApiError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{}: {}", self.code, self.message)
    }
}

impl std::error::Error for ApiError {}

fn api_error(code: &str, message: impl fmt::Display) -> ApiError {
    ApiError {
        code: code.into(),
        message: message.to_string(),
    }
}

/// The API error codes that can go away by themselves, for example while the server restarts.
const HEALABLE: [&str; 4] = ["unavailable", "unknown", "internal", "deadline_exceeded"];

/// Submits workflows. The token is an OIDC ID token, or a service account token with the audience "chronicle".
pub struct Client {
    server: String,
    token: String,
    agent: ureq::Agent,
}

impl Client {
    /// Returns a client for the server URL, for example `https://chronicle.example.com`.
    pub fn new(server: &str, token: &str) -> Result<Self, ApiError> {
        if !(server.starts_with("https://") || server.starts_with("http://")) {
            return Err(api_error(
                "invalid_argument",
                "the server URL must start with http:// or https://",
            ));
        }
        if token.is_empty() {
            return Err(api_error("invalid_argument", "give a token"));
        }
        let agent = ureq::Agent::config_builder()
            .http_status_as_error(false)
            .timeout_global(Some(Duration::from_secs(30)))
            .build()
            .into();
        Ok(Client {
            server: server.trim_end_matches('/').into(),
            token: token.into(),
            agent,
        })
    }

    /// Starts a run of the workflow in a namespace and returns the name of the run.
    pub fn submit(&self, namespace: &str, workflow: &Workflow) -> Result<String, ApiError> {
        let wf = workflow
            .to_value()
            .map_err(|e| api_error("invalid_argument", e))?;
        let answer = self.call(
            "WorkflowService",
            "SubmitWorkflow",
            json!({"namespace": namespace, "workflow": wf}),
            None,
        )?;
        Ok(answer["summary"]["name"]
            .as_str()
            .unwrap_or_default()
            .to_string())
    }

    /// Returns the phase of a run: Pending, Running, Succeeded, Failed, Error or Suspended.
    pub fn phase(&self, namespace: &str, name: &str) -> Result<String, ApiError> {
        let answer = self.call(
            "WorkflowService",
            "GetWorkflow",
            json!({"namespace": namespace, "name": name}),
            None,
        )?;
        Ok(answer["summary"]["phase"]
            .as_str()
            .unwrap_or_default()
            .to_string())
    }

    /// Reads a run every interval until it finishes, and returns its final phase: Succeeded, Failed or Error.
    /// A read that fails with an error that can heal, for example while the server restarts, is tried again.
    /// With a timeout, the error "deadline_exceeded" comes after that time.
    pub fn wait(
        &self,
        namespace: &str,
        name: &str,
        every: Duration,
        timeout: Option<Duration>,
    ) -> Result<String, ApiError> {
        let deadline = timeout.map(|t| Instant::now() + t);
        loop {
            match self.phase(namespace, name) {
                Ok(p) if matches!(p.as_str(), "Succeeded" | "Failed" | "Error") => return Ok(p),
                Err(e) if !HEALABLE.contains(&e.code.as_str()) => return Err(e),
                _ => {}
            }
            if deadline.is_some_and(|d| Instant::now() + every > d) {
                return Err(api_error(
                    "deadline_exceeded",
                    format!("the run {name} did not finish in time"),
                ));
            }
            thread::sleep(every);
        }
    }

    /// Calls a method of the API. timeout replaces the default of 30 s, for example for a long poll.
    pub(crate) fn call(
        &self,
        service: &str,
        method: &str,
        body: Value,
        timeout: Option<Duration>,
    ) -> Result<Value, ApiError> {
        let url = format!("{}/chronicle.v1.{service}/{method}", self.server);
        let mut req = self.agent.post(&url);
        if let Some(t) = timeout {
            req = req.config().timeout_global(Some(t)).build();
        }
        let mut resp = req
            .header("Authorization", format!("Bearer {}", self.token))
            .send_json(&body)
            .map_err(|e| api_error("unavailable", e))?;
        let status = resp.status();
        let answer: Value = resp
            .body_mut()
            .read_json()
            .map_err(|e| api_error("internal", e))?;
        if !status.is_success() {
            let code = answer["code"].as_str().unwrap_or("unknown");
            let message = answer["message"]
                .as_str()
                .unwrap_or("the server answered with an error");
            return Err(api_error(code, message));
        }
        Ok(answer)
    }
}
