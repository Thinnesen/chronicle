// Package controller has the reconcilers of chronicle. The Workflow reconciler reads the observed state (Pods, tasks,
// child runs, locks), asks the pure engine for the next actions and applies them. The CronWorkflow reconciler starts
// the runs of a schedule.
package controller
