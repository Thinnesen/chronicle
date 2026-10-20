//! The child workflow template.

use serde_json::{Map, Value, json};

use crate::{Kind, Template};

impl Template {
    /// Returns a template that starts a run of the WorkflowTemplate `template` in the same namespace and waits for
    /// it. Declare its outputs with `output_field`: they come from the workflow outputs of the child run.
    pub fn child_workflow(name: &str, template: &str) -> Self {
        let mut body = Map::new();
        body.insert("workflowTemplateRef".into(), json!({"name": template}));
        Template::new(name, Kind::ChildWorkflow, body)
    }

    fn child_body(&mut self) -> Option<&mut Map<String, Value>> {
        if self.kind != Kind::ChildWorkflow {
            self.error = Some(format!(
                "template {:?} is not a child workflow template",
                self.name
            ));
            return None;
        }
        Some(&mut self.body)
    }

    /// Makes a child workflow template start a ClusterWorkflowTemplate.
    pub fn cluster_scope(mut self) -> Self {
        if let Some(Value::Object(r)) = self
            .child_body()
            .and_then(|b| b.get_mut("workflowTemplateRef"))
        {
            r.insert("clusterScope".into(), true.into());
        }
        self
    }

    /// Gives a value to a parameter of the child run. The value can have placeholders.
    pub fn child_arg(mut self, name: &str, value: &str) -> Self {
        if let Some(body) = self.child_body() {
            let args = body
                .entry("arguments")
                .or_insert_with(|| json!({"parameters": []}));
            if let Some(Value::Array(list)) = args.get_mut("parameters") {
                list.push(json!({"name": name, "value": value}));
            }
        }
        self
    }
}
