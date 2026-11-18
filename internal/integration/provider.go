// Package integration menyiapkan integrasi master data dengan SIMRS tanpa
// menjadikannya ketergantungan: master data tetap lokal; sumber luar (CSV, dan
// SIMRS API/DB setelah spesifikasinya tersedia) dipetakan lewat external_id,
// tidak menimpa perubahan lokal, dan setiap proses tercatat.
package integration

import (
	"context"
	"errors"
)

// Entitas master data yang dapat disinkronkan.
const (
	EntityDepartment = "DEPARTMENT"
	EntityRoom       = "ROOM"
	EntityEmployee   = "EMPLOYEE"
	EntityAsset      = "ASSET"
)

// Entities dalam urutan impor yang disarankan (referensi lebih dulu).
var Entities = []string{EntityDepartment, EntityRoom, EntityEmployee, EntityAsset}

// ValidEntity memeriksa jenis entitas.
func ValidEntity(e string) bool {
	for _, x := range Entities {
		if x == e {
			return true
		}
	}
	return false
}

// Jenis provider (sumber data).
const (
	ProviderLocal    = "LOCAL"
	ProviderCSV      = "CSV"
	ProviderSimrsAPI = "SIMRS_API"
	ProviderSimrsDB  = "SIMRS_DB"
)

// Kebijakan konflik bila data lokal diubah manual setelah sinkron terakhir.
const (
	KeepLocal    = "KEEP_LOCAL"    // jangan timpa; catat sebagai konflik (default)
	PreferSource = "PREFER_SOURCE" // timpa dengan data sumber (keputusan eksplisit admin)
)

// Department adalah unit/instalasi dari sumber luar.
type Department struct {
	Row        int
	ExternalID string
	Code       string
	Name       string
	Critical   bool
	Active     bool
}

// Room adalah ruangan dari sumber luar. DepartmentRef = external_id unit di
// sumber yang sama, atau kode unit lokal.
type Room struct {
	Row           int
	ExternalID    string
	Code          string
	Name          string
	DepartmentRef string
	LocationNote  string
	Active        bool
}

// Employee adalah pegawai dari sumber luar (external_id = NIP).
type Employee struct {
	Row           int
	ExternalID    string
	Name          string
	DepartmentRef string
	Position      string
	Phone         string
	Active        bool
}

// Asset adalah perangkat/inventaris dari sumber luar. CategoryRef = kode atau
// nama kategori perangkat lokal; status perangkat tidak disinkronkan (dikelola
// lewat pemeliharaan).
type Asset struct {
	Row           int
	ExternalID    string
	Code          string
	Name          string
	CategoryRef   string
	DepartmentRef string
	RoomRef       string
	Brand         string
	Model         string
	Serial        string
}

// Antarmuka provider (docs/ARCHITECTURE.md §9). Satu provider boleh
// mengimplementasikan sebagian saja.
type (
	OrganizationProvider interface {
		Departments(ctx context.Context) ([]Department, error)
	}
	RoomProvider interface {
		Rooms(ctx context.Context) ([]Room, error)
	}
	EmployeeProvider interface {
		Employees(ctx context.Context) ([]Employee, error)
	}
	AssetProvider interface {
		Assets(ctx context.Context) ([]Asset, error)
	}
)

// Provider adalah sumber data terjadwal (mis. SIMRS API/DB).
type Provider interface {
	// Kind: ProviderSimrsAPI / ProviderSimrsDB.
	Kind() string
	// Source: label pemetaan external_id (mis. "SIMRS").
	Source() string
}

// ErrLocalSource: entitas dikelola lokal; tidak ada yang disinkronkan.
var ErrLocalSource = errors.New("sumber data lokal: data dikelola langsung di aplikasi, tidak ada sinkronisasi")

// ErrUnsupported: provider tidak menyediakan entitas ini.
var ErrUnsupported = errors.New("provider tidak menyediakan data untuk entitas ini")

// LocalProvider adalah implementasi awal: master data dikelola di aplikasi.
// Sinkronisasi tidak melakukan apa pun dan menyatakannya dengan jujur.
type LocalProvider struct{}

// Kind mengembalikan ProviderLocal.
func (LocalProvider) Kind() string { return ProviderLocal }

// Source untuk LocalProvider tidak dipakai.
func (LocalProvider) Source() string { return "LOCAL" }

// Departments selalu ErrLocalSource.
func (LocalProvider) Departments(context.Context) ([]Department, error) { return nil, ErrLocalSource }

// Rooms selalu ErrLocalSource.
func (LocalProvider) Rooms(context.Context) ([]Room, error) { return nil, ErrLocalSource }

// Employees selalu ErrLocalSource.
func (LocalProvider) Employees(context.Context) ([]Employee, error) { return nil, ErrLocalSource }

// Assets selalu ErrLocalSource.
func (LocalProvider) Assets(context.Context) ([]Asset, error) { return nil, ErrLocalSource }

// Batch adalah satu set data hasil ambil dari provider atau CSV.
type Batch struct {
	Entity      string
	Departments []Department
	Rooms       []Room
	Employees   []Employee
	Assets      []Asset
	// RowErrors: baris CSV yang tidak dapat dibaca (nomor baris → pesan).
	RowErrors map[int]string
}

// Len adalah jumlah baris valid di batch.
func (b Batch) Len() int {
	return len(b.Departments) + len(b.Rooms) + len(b.Employees) + len(b.Assets)
}

// fetch mengambil data entitas dari provider.
func fetch(ctx context.Context, p Provider, entity string) (Batch, error) {
	b := Batch{Entity: entity}
	var err error
	switch entity {
	case EntityDepartment:
		pp, ok := p.(OrganizationProvider)
		if !ok {
			return b, ErrUnsupported
		}
		b.Departments, err = pp.Departments(ctx)
	case EntityRoom:
		pp, ok := p.(RoomProvider)
		if !ok {
			return b, ErrUnsupported
		}
		b.Rooms, err = pp.Rooms(ctx)
	case EntityEmployee:
		pp, ok := p.(EmployeeProvider)
		if !ok {
			return b, ErrUnsupported
		}
		b.Employees, err = pp.Employees(ctx)
	case EntityAsset:
		pp, ok := p.(AssetProvider)
		if !ok {
			return b, ErrUnsupported
		}
		b.Assets, err = pp.Assets(ctx)
	default:
		return b, ErrUnsupported
	}
	return b, err
}
