//! The http template.

use serde_json::{Map, Value, json};

use crate::{Kind, Template};

impl Template {
    /// Returns a template that sends one request from a Pod, with the method GET. A status from 200 to 299 is
    /// success, and the response body is the result. The URL can have placeholders.
    pub fn http(name: &str, url: &str) -> Self {
        let mut body = Map::new();
        body.insert("url".into(), url.into());
        Template::new(name, Kind::Http, body)
    }

    fn http_field(mut self, key: &str, value: Value) -> Self {
        if self.kind != Kind::Http {
            self.error = Some(format!("template {:?} is not an http template", self.name));
            return self;
        }
        match (self.body.get_mut(key), value) {
            (Some(Value::Array(list)), Value::Array(more)) => list.extend(more),
            (_, value) => {
                self.body.insert(key.into(), value);
            }
        }
        self
    }

    /// Sets the method of the request of an http template, for example "POST".
    pub fn method(self, method: &str) -> Self {
        self.http_field("method", method.into())
    }

    /// Sets the body of the request of an http template. It can have placeholders.
    pub fn body(self, body: &str) -> Self {
        self.http_field("body", body.into())
    }

    /// Adds a header to the request of an http template.
    pub fn header(self, name: &str, value: &str) -> Self {
        self.http_field("headers", json!([{"name": name, "value": value}]))
    }

    /// Adds a header whose value is the key of a Secret in the namespace, for example a token.
    pub fn header_from_secret(self, name: &str, secret: &str, key: &str) -> Self {
        let header =
            json!([{"name": name, "valueFrom": {"secretKeyRef": {"name": secret, "key": key}}}]);
        self.http_field("headers", header)
    }
}
