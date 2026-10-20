//! The continueOn field of a task.

use serde_json::{Map, Value};

use crate::Task;

impl Task {
    /// Lets the run go on after the task fails (`failed`) or cannot run (`error`), as Argo's continueOn.
    pub fn continue_on(mut self, failed: bool, error: bool) -> Self {
        let mut on = Map::new();
        if failed {
            on.insert("failed".into(), Value::Bool(true));
        }
        if error {
            on.insert("error".into(), Value::Bool(true));
        }
        self.fields.insert("continueOn".into(), Value::Object(on));
        self
    }
}
