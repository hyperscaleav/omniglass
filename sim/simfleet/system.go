package simfleet

import "github.com/hyperscaleav/omniglass/internal/systemplan"

// The system planner is the shared, pure half of the planned-system primitive
// (internal/systemplan); the simulator plans with the products it serves.
type (
	Slot       = systemplan.Slot
	Product    = systemplan.Product
	SlotFill   = systemplan.SlotFill
	SystemPlan = systemplan.SystemPlan
)

// SystemCatalog is what the simulator's plan consults: whether the simulator
// can serve a product, and whether a component type falls within an
// accepted type (self-inclusive).
type SystemCatalog struct {
	Simulated func(product string) bool
	Within    func(componentType, accepted string) bool
}

// PlanSystem staffs a standard's slots with simulated products: the shared
// planner (systemplan.PlanSystem) with the simulator's products as every
// slot's candidates. See systemplan.PlanSystem for the rules.
func PlanSystem(slots []Slot, products []Product, cat SystemCatalog) SystemPlan {
	return systemplan.PlanSystem(slots, products, systemplan.Catalog{
		Candidate: func(_, product string) bool { return cat.Simulated(product) },
		Within:    cat.Within,
		Noun:      "simulated",
	})
}
