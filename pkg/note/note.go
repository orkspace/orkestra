// Package note provides the Orkestra template function library.
//
// A note is a pure, named transformation function available in every
// template expression across the Orkestra runtime — conversion paths,
// status fields, mutation rules, when conditions, onCreate templates.
//
// The name aligns with Orkestra's musical identity. Notes are the atomic
// units from which music is composed. In Orkestra, notes are the atomic
// units from which declarative operator behavior is composed — small,
// precise, and available everywhere a template expression is evaluated.
//
// Notes are pure functions. They receive values and return transformed
// values. They cannot perform I/O, call external APIs, or produce side
// effects. For those, use Go hooks.
//
// Usage in a Katalog:
//
//	conversion:
//	  paths:
//	    - from: v1
//	      to: v2
//	      spec:
//	        schedule: "{{ cronToMap .spec.schedule }}"
//
//	status:
//	  fields:
//	    - path: environment
//	      value: "{{ toLower .spec.environment }}"
//	    - path: replicas
//	      value: "{{ default .spec.replicas 2 }}"
//	    - path: schedule
//	      value: "{{ cronFromAny .spec.schedule }}"
package note

import "text/template"

// notes is the package-level function map.
// Built once at init time — safe for concurrent use.
// Referenced by Map() which is called by the resolver.
var notes = buildNotes()

// Map returns the complete Orkestra note library as a template.FuncMap.
// The map is built once and returned on every call — no allocation overhead.
//
// Integrate into the resolver:
//
//	tmpl, err := template.New("f").
//	    Option("missingkey=zero").
//	    Funcs(note.Map()).
//	    Parse(value)
func Map() template.FuncMap {
	return notes
}

// buildNotes constructs the complete note map from all domains.
// Called once at package init.
func buildNotes() template.FuncMap {
	m := template.FuncMap{}
	register(m, asNotes())
	register(m, cronNotes())
	register(m, timeNotes())
	register(m, dataNotes())
	register(m, stringNotes())
	register(m, validationNotes())
	register(m, mathNotes())
	register(m, typeNotes())
	register(m, conditionalNotes())
	register(m, randomNotes())
	register(m, kubernetesNotes())
	register(m, kubeValidationNotes())
	register(m, quantityNotes())
	register(m, listMapNotes())
	register(m, safeAccessNotes())
	register(m, containerNotes())
	register(m, serviceNotes())
	register(m, replicaNotes())
	register(m, podNotes())
	register(m, jobNotes())
	register(m, eventNotes())
	register(m, pvcNotes())
	register(m, ingressNotes())
	register(m, hpaNotes())
	register(m, nodeNotes())
	register(m, replicaSetNotes())
	register(m, statefulSetNotes())
	register(m, fieldNotes())
	register(m, semverNotes())
	register(m, netNotes())
	register(m, domainNotes())
	register(m, prometheusNotes())
	register(m, serveNotes())
	return m
}

// register merges src into dst.
func register(dst, src template.FuncMap) {
	for k, v := range src {
		dst[k] = v
	}
}
