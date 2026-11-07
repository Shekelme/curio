package harmonytask

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/harmony/harmonytask/internal/runregistry"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
)

func TestDualMemoryCapacity(t *testing.T) {
	const gib = uint64(1 << 30)
	reg := &resources.Reg{Resources: resources.Resources{
		Cpu:         16,
		Ram:         64 * gib,
		RamPhysical: 64 * gib,
		RamVirtual:  128 * gib,
	}}

	max := taskhelp.Max(8)
	h := &taskTypeHandler{
		TaskTypeDetails: TaskTypeDetails{
			Name: "DualMemory",
			Max:  max,
			Cost: resources.Resources{
				RamMin: 32 * gib,
				RamMax: 64 * gib,
			},
		},
	}
	e := &TaskEngine{cfg: taskEngineConfig{reg: reg}, handlers: []*taskTypeHandler{h}}
	h.TaskEngine = e

	headroom, err := h.AssertMachineHasCapacity()
	require.NoError(t, err)
	require.Equal(t, 2, headroom)

	// Once one task is active, exactly one additional task still fits in
	// both dimensions: 32 GiB physical and 64 GiB virtual remain.
	max.Add(1)
	available := e.ResourcesAvailable()
	require.Equal(t, 32*gib, available.RamPhysical)
	require.Equal(t, 64*gib, available.RamVirtual)

	headroom, err = h.AssertMachineHasCapacity()
	require.NoError(t, err)
	require.Equal(t, 1, headroom)

	// A third task must not fit: the virtual reservation is exhausted after
	// two tasks even though the physical minimum would permit more.
	max.Add(1)
	_, err = h.AssertMachineHasCapacity()
	require.Error(t, err)
}

func TestDualMemoryPreemptionUsesBothDimensions(t *testing.T) {
	const gib = uint64(1 << 30)
	max := taskhelp.Max(8)
	victim := &taskTypeHandler{
		TaskTypeDetails: TaskTypeDetails{
			Name: "DualMemoryVictim",
			Max:  max,
			Cost: resources.Resources{
				Cpu:    1,
				RamMin: 32 * gib,
				RamMax: 64 * gib,
			},
		},
		running: runregistry.New(),
	}
	victim.running.Start(42, func() {})
	max.Add(1)

	e := &TaskEngine{
		cfg: taskEngineConfig{reg: &resources.Reg{Resources: resources.Resources{
			Cpu:         8,
			Ram:         128 * gib,
			RamPhysical: 128 * gib,
			RamVirtual:  256 * gib,
		}}},
		handlers: []*taskTypeHandler{victim},
	}

	plan := e.computePreemptionPlan(resources.Resources{
		Cpu:    1,
		RamMin: 100 * gib,
		RamMax: 200 * gib,
	})
	require.NotNil(t, plan)
	require.Len(t, plan.candidates, 1)
	require.Equal(t, TaskID(42), plan.candidates[0].taskID)
}

func TestLegacyRamCostRemainsSingleDimensionCompatible(t *testing.T) {
	const gib = uint64(1 << 30)
	h := &taskTypeHandler{
		TaskTypeDetails: TaskTypeDetails{
			Name: "LegacyMemory",
			Max:  taskhelp.Max(8),
			Cost: resources.Resources{Ram: 64 * gib},
		},
	}
	e := &TaskEngine{cfg: taskEngineConfig{reg: &resources.Reg{Resources: resources.Resources{
		Ram: 128 * gib,
	}}}, handlers: []*taskTypeHandler{h}}
	h.TaskEngine = e

	headroom, err := h.AssertMachineHasCapacity()
	require.NoError(t, err)
	require.Equal(t, 2, headroom)
}

func TestSDRReservationsAllowTwoTasksWithSwapHeadroom(t *testing.T) {
	const gib = uint64(1 << 30)
	const ramMin = 60 * gib
	const ramMax = 64*gib + (256 << 20)

	// Representative burstmine sizing: 125 GiB physical available and
	// 252 GiB total virtual available (physical + free swap).
	reg := &resources.Reg{Resources: resources.Resources{
		Cpu:         32,
		Ram:         125 * gib,
		RamPhysical: 125 * gib,
		RamVirtual:  252 * gib,
	}}

	max := taskhelp.Max(8)
	h := &taskTypeHandler{
		TaskTypeDetails: TaskTypeDetails{
			Name: "SDR",
			Max:  max,
			Cost: resources.Resources{RamMin: ramMin, RamMax: ramMax},
		},
	}
	e := &TaskEngine{cfg: taskEngineConfig{reg: reg}, handlers: []*taskTypeHandler{h}}
	h.TaskEngine = e

	headroom, err := h.AssertMachineHasCapacity()
	require.NoError(t, err)
	require.Equal(t, 2, headroom)

	max.Add(2)
	_, err = h.AssertMachineHasCapacity()
	require.Error(t, err, "a third SDR must not fit")
}
