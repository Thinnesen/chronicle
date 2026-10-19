//! The client for the chronicle API server. It uses the Connect protocol with JSON.

use std::fmt;
use std::time::Duration;

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

/// Submits workflows. The token is an OIDC ID token, or a service account token with the audience "chronicle".
pub struct Client {
    server: String,
    token: String,
    agent: ureq::Agent,
}

impl Client {
    /// Returns a client for the server URL, for example "https://chronicle.example.com".
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
            "SubmitWorkflow",
            json!({"namespace": namespace, "workflow": wf}),
        )?;
        Ok(answer["summary"]["name"]
            .as_str()
            .unwrap_or_default()
            .to_string())
    }

    /// Returns the phase of a run: Pending, Running, Succeeded, Failed, Error or Suspended.
    pub fn phase(&self, namespace: &str, name: &str) -> Result<String, ApiError> {
        let answer = self.call("GetWorkflow", json!({"namespace": namespace, "name": name}))?;
        Ok(answer["summary"]["phase"]
            .as_str()
            .unwrap_or_default()
            .to_string())
    }

    fn call(&self, method: &str, body: Value) -> Result<Value, ApiError> {
        let url = format!("{}/chronicle.v1.WorkflowService/{method}", self.server);
        let mut resp = self
            .agent
            .post(&url)
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
