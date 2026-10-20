package expr

// Strftime maps the strftime letters that Argo has for {{workflow.creationTimestamp.<letter>}} to Go time layouts.
var Strftime = map[string]string{
	"B": "January", "b": "Jan", "m": "01", "A": "Monday", "a": "Mon", "d": "02", "H": "15", "I": "03",
	"M": "04", "S": "05", "Y": "2006", "y": "06", "p": "PM", "Z": "MST", "z": "-0700", "L": ".000",
}
