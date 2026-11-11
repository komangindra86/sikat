package settings

import (
	"net/http"

	"sikat/internal/reqctx"
	"sikat/internal/web"
)

// Handler menyediakan halaman pengaturan sistem.
type Handler struct {
	Service  *Service
	Renderer *web.Renderer
}

type pageData struct {
	Groups []group
}

type group struct {
	Name  string
	Items []Item
}

func groupItems(items []Item) []group {
	var out []group
	idx := map[string]int{}
	for _, it := range items {
		i, ok := idx[it.Group]
		if !ok {
			idx[it.Group] = len(out)
			out = append(out, group{Name: it.Group})
			i = len(out) - 1
		}
		out[i].Items = append(out[i].Items, it)
	}
	return out
}

// Show menampilkan pengaturan.
func (h *Handler) Show(w http.ResponseWriter, r *http.Request) {
	items, err := h.Service.List(r.Context())
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	h.Renderer.Render(w, r, http.StatusOK, "settings/index", web.Page{
		Title: "Pengaturan Sistem", Nav: "settings", Data: pageData{Groups: groupItems(items)},
	})
}

// Save menyimpan pengaturan.
func (h *Handler) Save(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.Renderer.Error(w, r, http.StatusBadRequest, "")
		return
	}
	values := map[string]string{}
	for _, d := range Defs {
		if v, ok := r.PostForm[d.Key]; ok && len(v) > 0 {
			values[d.Key] = v[0]
		}
	}
	errs, err := h.Service.Update(r.Context(), reqctx.UserFrom(r.Context()).ID, values)
	if err != nil {
		h.Renderer.ServerError(w, r, err)
		return
	}
	if len(errs) > 0 {
		items := make([]Item, 0, len(Defs))
		for _, d := range Defs {
			items = append(items, Item{Def: d, Value: values[d.Key]})
		}
		h.Renderer.Render(w, r, http.StatusUnprocessableEntity, "settings/index", web.Page{
			Title: "Pengaturan Sistem", Nav: "settings", Errors: errs, Data: pageData{Groups: groupItems(items)},
		})
		return
	}
	h.Renderer.Redirect(w, r, "/app/settings", "success", "Pengaturan berhasil disimpan.")
}
