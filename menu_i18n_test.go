package main

import (
	"testing"

	"github.com/wailsapp/wails/v2/pkg/menu"
)

func menuLabels(m *menu.Menu, out *[]string) {
	if m == nil {
		return
	}
	for _, it := range m.Items {
		if it.Label != "" {
			*out = append(*out, it.Label)
		}
		menuLabels(it.SubMenu, out)
	}
}

func TestMenuRussian(t *testing.T) {
	var en, ru []string
	menuLabels(buildMenu(&App{}, ""), &en)
	menuLabels(buildMenu(&App{}, "ru"), &ru)
	if len(en) == 0 || len(en) != len(ru) {
		t.Fatalf("menus differ: %d English, %d Russian labels", len(en), len(ru))
	}
	for i := range en {
		if en[i] == ru[i] {
			if _, ok := menuRU[en[i]]; !ok {
				t.Errorf("menu label %q has no Russian text", en[i])
			}
		}
	}
	if menuText("ru", "Exit") != "Выход" || menuText("en", "Exit") != "Exit" {
		t.Error("menuText")
	}
}
