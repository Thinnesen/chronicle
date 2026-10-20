//! The workflow object: its metadata, parameters and templates.

use std::collections::BTreeMap;

use serde_json::{Map, Value, json};

use crate::{API_VERSION, Template, WorkflowError};

/// A workflow definition. Render it with [`Workflow::to_value`] or submit it with a [`Client`](crate::Client).
#[derive(Debug, Clone, Default)]
pub struct Workflow {
    metadata: Map<String, Value>,
    params: Vec<Value>,
    spec: Map<String, Value>,
    entrypoint: Option<Template>,
    on_exit: Option<Template>,
}

impl Workflow {
    /// Returns a workflow whose runs get the name prefix generate_name, for example "etl-".
    pub fn new(generate_name: &str) -> Self {
        let mut w = Workflow::default();
        w.metadata
            .insert("generateName".into(), generate_name.into());
        w
    }

    /// Gives the run a fixed name instead of a generated one.
    pub fn name(mut self, name: &str) -> Self {
        self.metadata.remove("generateName");
        self.metadata.insert("name".into(), name.into());
        self
    }

    /// Adds a label to the run.
    pub fn label(mut self, key: &str, value: &str) -> Self {
        if let Value::Object(labels) = self.metadata.entry("labels").or_insert_with(|| json!({})) {
            labels.insert(key.into(), value.into());
        }
        self
    }

    /// Limits the Pods of a run that run at the same time.
    pub fn parallelism(mut self, n: u32) -> Self {
        self.spec.insert("parallelism".into(), n.into());
        self
    }

    /// Lets one run with the lock name run at a time in the namespace. Other runs wait.
    pub fn mutex(mut self, name: &str) -> Self {
        self.spec
            .insert("synchronization".into(), json!({"mutex": {"name": name}}));
        self
    }

    /// Lets limit runs with the lock name run at the same time in the namespace. Other runs wait.
    pub fn semaphore(mut self, name: &str, limit: u32) -> Self {
        self.spec.insert(
            "synchronization".into(),
            json!({"semaphore": {"name": name, "limit": limit}}),
        );
        self
    }

    /// Like `semaphore`, with the limit in a key of a ConfigMap. The ConfigMap needs the label
    /// `chronicle.thinnesen.com/parameters: "true"`.
    pub fn semaphore_from(mut self, name: &str, config_map: &str, key: &str) -> Self {
        self.spec.insert(
            "synchronization".into(),
            json!({"semaphore": {"name": name, "configMapKeyRef": {"name": config_map, "key": key}}}),
        );
        self
    }

    /// Runs the workflow Pods with a Kubernetes service account.
    pub fn service_account(mut self, name: &str) -> Self {
        self.spec.insert("serviceAccountName".into(), name.into());
        self
    }

    /// Declares a workflow parameter with a default value. Use [`param`](crate::param) for its placeholder.
    pub fn param(mut self, name: &str, value: &str) -> Self {
        self.params.push(json!({"name": name, "value": value}));
        self
    }

    /// Sets the template that the run starts with.
    pub fn entrypoint(mut self, template: Template) -> Self {
        self.entrypoint = Some(template);
        self
    }

    /// Sets a template that runs after the entrypoint, also after a failure.
    pub fn on_exit(mut self, template: Template) -> Self {
        self.on_exit = Some(template);
        self
    }

    /// Returns the Workflow object. The server checks it completely when you submit it.
    pub fn to_value(&self) -> Result<Value, WorkflowError> {
        let entry = self
            .entrypoint
            .as_ref()
            .ok_or_else(|| WorkflowError("the workflow has no entrypoint".into()))?;
        let mut templates: BTreeMap<String, (usize, Template)> = BTreeMap::new();
        collect(entry, &mut templates)?;
        let mut spec = self.spec.clone();
        spec.insert("entrypoint".into(), entry.name.clone().into());
        if let Some(exit) = &self.on_exit {
            collect(exit, &mut templates)?;
            spec.insert("onExit".into(), exit.name.clone().into());
        }
        if !self.params.is_empty() {
            spec.insert("arguments".into(), json!({"parameters": self.params}));
        }
        let mut ordered: Vec<&(usize, Template)> = templates.values().collect();
        ordered.sort_by_key(|(i, _)| *i);
        let list: Vec<Value> = ordered.iter().map(|(_, t)| t.to_value()).collect();
        spec.insert("templates".into(), Value::Array(list));
        Ok(
            json!({"apiVersion": API_VERSION, "kind": "Workflow", "metadata": self.metadata, "spec": spec}),
        )
    }

    /// Returns the Workflow object as JSON. kubectl and GitOps tools accept JSON as YAML.
    pub fn to_json(&self) -> Result<String, WorkflowError> {
        let v = self.to_value()?;
        Ok(serde_json::to_string_pretty(&v).expect("a JSON value always encodes"))
    }
}

/// collect adds a template and the templates of its tasks, in the order of first use.
fn collect(
    t: &Template,
    seen: &mut BTreeMap<String, (usize, Template)>,
) -> Result<(), WorkflowError> {
    if let Some(e) = &t.error {
        return Err(WorkflowError(e.clone()));
    }
    if let Some((_, prev)) = seen.get(&t.name) {
        if prev != t {
            return Err(WorkflowError(format!(
                "two different templates have the name {:?}",
                t.name
            )));
        }
        return Ok(());
    }
    let index = seen.len();
    seen.insert(t.name.clone(), (index, t.clone()));
    for task in &t.tasks {
        collect(&task.template, seen)?;
        if let Some(hook) = &task.hook {
            collect(hook, seen)?;
        }
    }
    Ok(())
}
