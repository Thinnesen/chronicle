//! The tasks of a DAG.

use serde_json::{Map, Value, json};

use crate::Template;

/// A task of a DAG. It runs a template, and its methods return placeholders for its outputs.
#[derive(Debug, Clone, PartialEq)]
pub struct Task {
    pub(crate) name: String,
    pub(crate) template: Box<Template>,
    pub(crate) hook: Option<Box<Template>>,
    pub(crate) fields: Map<String, Value>,
    params: Vec<Value>,
    artifacts: Vec<Value>,
}

impl Task {
    /// Returns a task that runs template. Configure the template before you make the task.
    pub fn new(name: &str, template: &Template) -> Self {
        Task {
            name: name.to_string(),
            template: Box::new(template.clone()),
            hook: None,
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

    /// Runs hook after the task ends, also after a failure. The tasks after it wait for the hook. args are the
    /// parameter arguments of the hook; they can use the outputs and the status of the task.
    pub fn on_exit(mut self, hook: &Template, args: &[(&str, &str)]) -> Self {
        let mut h = Map::new();
        h.insert("template".into(), hook.name.clone().into());
        let mut sorted = args.to_vec();
        sorted.sort();
        if !sorted.is_empty() {
            let params: Vec<Value> = sorted
                .iter()
                .map(|(k, v)| json!({"name": k, "value": v}))
                .collect();
            h.insert("arguments".into(), json!({ "parameters": params }));
        }
        self.fields.insert("onExit".into(), Value::Object(h));
        self.hook = Some(Box::new(hook.clone()));
        self
    }

    /// Returns the placeholder for the phase of the task, for example in an exit hook.
    pub fn status(&self) -> String {
        format!("{{{{tasks.{}.status}}}}", self.name)
    }

    /// Runs the task once for each item. The template uses "{{item}}" or "{{item.key}}".
    pub fn with_items<I: IntoIterator<Item = Value>>(mut self, items: I) -> Self {
        self.fields.insert(
            "withItems".into(),
            Value::Array(items.into_iter().collect()),
        );
        self
    }

    /// Runs the task once for each number from start, count times. count can be a placeholder.
    /// format makes the item text, for example "part-%d"; empty is "%d".
    pub fn with_sequence(mut self, count: &str, start: i64, format: &str) -> Self {
        let mut seq = Map::new();
        seq.insert("count".into(), count.into());
        if start != 0 {
            seq.insert("start".into(), start.into());
        }
        if !format.is_empty() {
            seq.insert("format".into(), format.into());
        }
        self.fields
            .insert("withSequence".into(), Value::Object(seq));
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

    pub(crate) fn to_value(&self) -> Value {
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
