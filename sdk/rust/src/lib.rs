//! Define chronicle workflows as Rust code, render them as JSON and submit them.
//!
//! Build templates with [`Template::container`], [`Template::script`] and [`Template::dag`]. Make tasks with
//! [`Task::new`]. Use the placeholders that [`param`], [`input`] and the [`Task`] outputs return to connect the steps.

use std::collections::BTreeMap;
use std::fmt;

use serde_json::{Map, Value, json};

mod client;
pub use client::{ApiError, Client};

/// The API version of chronicle Workflow objects.
pub const API_VERSION: &str = "chronicle.thinnesen.com/v1alpha1";

/// A mistake in a workflow definition.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct WorkflowError(pub String);

impl fmt::Display for WorkflowError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(&self.0)
    }
}

impl std::error::Error for WorkflowError {}

/// Returns the placeholder for a workflow parameter.
pub fn param(name: &str) -> String {
    format!("{{{{workflow.parameters.{name}}}}}")
}

/// Returns the placeholder for an input parameter of the template.
pub fn input(name: &str) -> String {
    format!("{{{{inputs.parameters.{name}}}}}")
}

#[derive(Debug, Clone, PartialEq)]
enum Kind {
    Container,
    Script,
    Dag,
}

/// A step definition: a container, a script or a DAG of tasks.
#[derive(Debug, Clone, PartialEq)]
pub struct Template {
    name: String,
    kind: Kind,
    body: Map<String, Value>,
    inputs: Map<String, Value>,
    outputs: Map<String, Value>,
    extra: Map<String, Value>,
    tasks: Vec<Task>,
    error: Option<String>,
}

impl Template {
    fn new(name: &str, kind: Kind, body: Map<String, Value>) -> Self {
        Template {
            name: name.to_string(),
            kind,
            body,
            inputs: Map::new(),
            outputs: Map::new(),
            extra: Map::new(),
            tasks: Vec::new(),
            error: None,
        }
    }

    /// Returns a template that runs an image with a command.
    pub fn container<I, S>(name: &str, image: &str, command: I) -> Self
    where
        I: IntoIterator<Item = S>,
        S: Into<String>,
    {
        let mut body = Map::new();
        body.insert("image".into(), image.into());
        let command: Vec<String> = command.into_iter().map(Into::into).collect();
        if !command.is_empty() {
            body.insert("command".into(), json!(command));
        }
        Template::new(name, Kind::Container, body)
    }

    /// Returns a template that runs a shell script. A script reads its inputs from environment variables.
    pub fn script(name: &str, image: &str, source: &str) -> Self {
        let mut t = Template::new(name, Kind::Script, Map::new());
        t.body.insert("image".into(), image.into());
        t.body.insert("source".into(), source.into());
        if source.contains("{{") {
            t.error = Some(format!(
                "template {name:?}: a script source cannot have placeholders, use env"
            ));
        }
        t
    }

    /// Returns a template that runs tasks in the order of their dependencies.
    pub fn dag(name: &str) -> Self {
        Template::new(name, Kind::Dag, Map::new())
    }

    /// Returns the name of the template.
    pub fn name(&self) -> &str {
        &self.name
    }

    fn container_field(mut self, key: &str, value: Value) -> Self {
        if self.kind == Kind::Dag {
            self.error = Some(format!("template {:?} has no container", self.name));
            return self;
        }
        match (self.body.get_mut(key), value) {
            (Some(Value::Array(list)), Value::Array(more)) if key == "env" => list.extend(more),
            (_, value) => {
                self.body.insert(key.into(), value);
            }
        }
        self
    }

    /// Sets the arguments of the command.
    pub fn args<I, S>(self, args: I) -> Self
    where
        I: IntoIterator<Item = S>,
        S: Into<String>,
    {
        let args: Vec<String> = args.into_iter().map(Into::into).collect();
        self.container_field("args", json!(args))
    }

    /// Adds an environment variable. Use it to give inputs to a script.
    pub fn env(self, name: &str, value: &str) -> Self {
        self.container_field("env", json!([{"name": name, "value": value}]))
    }

    fn push(map: &mut Map<String, Value>, key: &str, item: Value) {
        if let Value::Array(list) = map.entry(key).or_insert_with(|| json!([])) {
            list.push(item);
        }
    }

    /// Declares an input parameter. Use [`input`] for its placeholder.
    pub fn input(mut self, name: &str) -> Self {
        Self::push(&mut self.inputs, "parameters", json!({"name": name}));
        self
    }

    /// Declares an input parameter with a default value.
    pub fn input_default(mut self, name: &str, value: &str) -> Self {
        Self::push(
            &mut self.inputs,
            "parameters",
            json!({"name": name, "default": value}),
        );
        self
    }

    /// Declares an output parameter that gets the contents of a file in the container.
    pub fn output_file(mut self, name: &str, path: &str) -> Self {
        Self::push(
            &mut self.outputs,
            "parameters",
            json!({"name": name, "valueFrom": {"path": path}}),
        );
        self
    }

    /// Declares an input artifact that the executor puts at path.
    pub fn input_artifact(mut self, name: &str, path: &str) -> Self {
        Self::push(
            &mut self.inputs,
            "artifacts",
            json!({"name": name, "path": path}),
        );
        self
    }

    /// Declares an output artifact that the executor saves from path.
    pub fn output_artifact(mut self, name: &str, path: &str) -> Self {
        Self::push(
            &mut self.outputs,
            "artifacts",
            json!({"name": name, "path": path}),
        );
        self
    }

    /// Retries a failed node up to limit times. backoff is the first wait, for example "10s"; it doubles each time.
    pub fn retry(mut self, limit: u32, backoff: Option<&str>) -> Self {
        let mut strategy = json!({"limit": limit});
        if let Some(d) = backoff {
            strategy["backoff"] = json!({"duration": d, "factor": 2});
        }
        self.extra.insert("retryStrategy".into(), strategy);
        self
    }

    /// Fails a node of the template, or one attempt of a retry, after duration, for example "5m".
    pub fn timeout(mut self, duration: &str) -> Self {
        self.extra.insert("timeout".into(), duration.into());
        self
    }

    /// Adds tasks to a DAG template.
    pub fn tasks<I: IntoIterator<Item = Task>>(mut self, tasks: I) -> Self {
        if self.kind != Kind::Dag {
            self.error = Some(format!(
                "template {:?} is not a DAG: it cannot have tasks",
                self.name
            ));
            return self;
        }
        self.tasks.extend(tasks);
        self
    }

    fn to_value(&self) -> Value {
        let mut out = self.extra.clone();
        out.insert("name".into(), self.name.clone().into());
        match self.kind {
            Kind::Dag => {
                let tasks: Vec<Value> = self.tasks.iter().map(Task::to_value).collect();
                out.insert("dag".into(), json!({"tasks": tasks}));
            }
            Kind::Container => {
                out.insert("container".into(), Value::Object(self.body.clone()));
            }
            Kind::Script => {
                out.insert("script".into(), Value::Object(self.body.clone()));
            }
        }
        if !self.inputs.is_empty() {
            out.insert("inputs".into(), Value::Object(self.inputs.clone()));
        }
        if !self.outputs.is_empty() {
            out.insert("outputs".into(), Value::Object(self.outputs.clone()));
        }
        Value::Object(out)
    }
}

/// A task of a DAG. It runs a template, and its methods return placeholders for its outputs.
#[derive(Debug, Clone, PartialEq)]
pub struct Task {
    name: String,
    template: Box<Template>,
    fields: Map<String, Value>,
    params: Vec<Value>,
    artifacts: Vec<Value>,
}

impl Task {
    /// Returns a task that runs template. Configure the template before you make the task.
    pub fn new(name: &str, template: &Template) -> Self {
        Task {
            name: name.to_string(),
            template: Box::new(template.clone()),
            fields: Map::new(),
            params: Vec::new(),
            artifacts: Vec::new(),
        }
    }

    /// Waits until the other tasks have succeeded or were skipped.
    pub fn after(mut self, tasks: &[&Task]) -> Self {
        for t in tasks {
            Template::push(&mut self.fields, "dependencies", t.name.clone().into());
        }
        self
    }

    /// Sets a dependency expression, for example "build.Succeeded || build.Failed".
    pub fn depends(mut self, expression: &str) -> Self {
        self.fields.insert("depends".into(), expression.into());
        self
    }

    /// Gives a value to an input parameter of the template.
    pub fn arg(mut self, name: &str, value: &str) -> Self {
        self.params.push(json!({"name": name, "value": value}));
        self
    }

    /// Gives an artifact, from the [`Task::artifact`] placeholder of another task, to an input artifact.
    pub fn artifact_arg(mut self, name: &str, from: &str) -> Self {
        self.artifacts.push(json!({"name": name, "from": from}));
        self
    }

    /// Runs the task only when the condition is true.
    pub fn when(mut self, condition: &str) -> Self {
        self.fields.insert("when".into(), condition.into());
        self
    }

    /// Runs the task once for each item. The template uses "{{item}}" or "{{item.key}}".
    pub fn with_items<I: IntoIterator<Item = Value>>(mut self, items: I) -> Self {
        self.fields.insert(
            "withItems".into(),
            Value::Array(items.into_iter().collect()),
        );
        self
    }

    /// Returns the placeholder for the standard output of the task.
    pub fn result(&self) -> String {
        format!("{{{{tasks.{}.outputs.result}}}}", self.name)
    }

    /// Returns the placeholder for an output parameter of the task.
    pub fn output(&self, name: &str) -> String {
        format!("{{{{tasks.{}.outputs.parameters.{name}}}}}", self.name)
    }

    /// Returns the placeholder for an output artifact of the task, for [`Task::artifact_arg`].
    pub fn artifact(&self, name: &str) -> String {
        format!("{{{{tasks.{}.outputs.artifacts.{name}}}}}", self.name)
    }

    fn to_value(&self) -> Value {
        let mut out = self.fields.clone();
        out.insert("name".into(), self.name.clone().into());
        out.insert("template".into(), self.template.name.clone().into());
        let mut args = Map::new();
        if !self.params.is_empty() {
            args.insert("parameters".into(), Value::Array(self.params.clone()));
        }
        if !self.artifacts.is_empty() {
            args.insert("artifacts".into(), Value::Array(self.artifacts.clone()));
        }
        if !args.is_empty() {
            out.insert("arguments".into(), Value::Object(args));
        }
        Value::Object(out)
    }
}

/// A workflow definition. Render it with [`Workflow::to_value`] or submit it with a [`Client`].
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

    /// Runs the workflow Pods with a Kubernetes service account.
    pub fn service_account(mut self, name: &str) -> Self {
        self.spec.insert("serviceAccountName".into(), name.into());
        self
    }

    /// Declares a workflow parameter with a default value. Use [`param`] for its placeholder.
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
    }
    Ok(())
}
