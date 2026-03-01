package main

import (
	"context"
	"encoding/json"
	"log"
	"os"

	"github.com/fy0/gorbac/v3"
)

func init() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
}

func LoadJson(filename string, v interface{}) error {
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewDecoder(f).Decode(v)
}

func SaveJson(filename string, v interface{}) error {
	f, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewEncoder(f).Encode(v)
}

func main() {
	ctx := context.Background()
	// map[RoleId]PermissionIds
	var jsonRoles map[string][]string
	// map[RoleId]ParentIds
	var jsonInher map[string][]string
	// Load roles information
	if err := LoadJson("roles.json", &jsonRoles); err != nil {
		log.Fatal(err)
	}
	// Load inheritance information
	if err := LoadJson("inher.json", &jsonInher); err != nil {
		log.Fatal(err)
	}
	rbac := gorbac.New[string]()
	permissions := make(map[string]gorbac.Permission[string])

	// Build roles and add them to goRBAC instance
	for rid, pids := range jsonRoles {
		role := gorbac.NewRole(rid)
		for _, pid := range pids {
			_, ok := permissions[pid]
			if !ok {
				permissions[pid] = gorbac.NewPermission(pid)
			}
			role.Assign(ctx, permissions[pid])
		}
		rbac.Add(ctx, role)
	}
	// Assign the inheritance relationship
	for rid, parents := range jsonInher {
		if err := rbac.SetParents(ctx, rid, parents...); err != nil {
			log.Fatal(err)
		}
	}
	// Check if `editor` can add text
	if rbac.IsGranted(ctx, "editor", permissions["add-text"]) {
		log.Println("Editor can add text")
	}
	// Check if `chief-editor` can add text
	if rbac.IsGranted(ctx, "chief-editor", permissions["add-text"]) {
		log.Println("Chief editor can add text")
	}
	// Check if `photographer` can add text
	if !rbac.IsGranted(ctx, "photographer", permissions["add-text"]) {
		log.Println("Photographer can't add text")
	}
	// Check if `nobody` can add text
	// `nobody` is not exist in goRBAC at the moment
	if !rbac.IsGranted(ctx, "nobody", permissions["read-text"]) {
		log.Println("Nobody can't read text")
	}
	// Add `nobody` and assign `read-text` permission
	nobody := gorbac.NewRole("nobody")
	permissions["read-text"] = gorbac.NewPermission("read-text")
	nobody.Assign(ctx, permissions["read-text"])
	rbac.Add(ctx, nobody)
	// Check if `nobody` can read text again
	if rbac.IsGranted(ctx, "nobody", permissions["read-text"]) {
		log.Println("Nobody can read text")
	}

	// Persist the change
	// map[RoleId]PermissionIds
	jsonOutputRoles := make(map[string][]string)
	// map[RoleId]ParentIds
	jsonOutputInher := make(map[string][]string)
	SaveJsonHandler := func(r gorbac.Role[string], parents []string) error {
		// WARNING: Don't use gorbac.RBAC instance in the handler,
		// otherwise it causes deadlock.
		permissions := make([]string, 0)
		for _, p := range r.Permissions(ctx) {
			permissions = append(permissions, p.ID())
		}
		jsonOutputRoles[r.ID()] = permissions
		jsonOutputInher[r.ID()] = parents
		return nil
	}
	if err := gorbac.Walk(ctx, rbac, SaveJsonHandler); err != nil {
		log.Fatalln(err)
	}

	// Save roles information
	if err := SaveJson("new-roles.json", &jsonOutputRoles); err != nil {
		log.Fatal(err)
	}
	// Save inheritance information
	if err := SaveJson("new-inher.json", &jsonOutputInher); err != nil {
		log.Fatal(err)
	}
}
