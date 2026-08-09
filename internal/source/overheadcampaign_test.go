package source

import (
	"fmt"
	"math"
	"runtime"
	"slices"
	"testing"
)

type overheadCampaignRun struct {
	Median   float64
	Measured bool
	Stderr   string
}

type overheadCampaignRecord struct {
	CPUModel, ContainerRuntime, PostgreSQLVersion string
	Cores, Attempted, Measured                    int
	Maximum, Published                            float64
}

func recordOverheadCampaign(runs []overheadCampaignRun, cpu, runtimeName, postgres string, cores int) (overheadCampaignRecord, error) {
	record := overheadCampaignRecord{CPUModel: cpu, ContainerRuntime: runtimeName, PostgreSQLVersion: postgres,
		Cores: cores, Attempted: len(runs)}
	var measured []float64
	for _, run := range runs {
		if !run.Measured {
			continue
		}
		record.Measured++
		measured = append(measured, run.Median)
	}
	if record.Measured < overheadLeastPairs {
		return record, fmt.Errorf("the overhead ceiling was not measured on this run: %d of %d runs measured", record.Measured, record.Attempted)
	}
	record.Maximum = slices.Max(measured)
	record.Published = math.Ceil(record.Maximum*10) / 10
	if !slices.Contains([]float64{1.5, 1.6, 1.7, 1.8, 1.9}, record.Published) {
		return record, fmt.Errorf("maximum median %.3f rounds to unpublished ceiling %.1f", record.Maximum, record.Published)
	}
	return record, nil
}

func TestOverheadCampaignCountsMeasuredRunsAndDerivesPublishedCeiling(t *testing.T) {
	runs := []overheadCampaignRun{{Median: 1.51, Measured: true}, {Stderr: "the overhead ceiling was not measured"},
		{Median: 1.674, Measured: true}, {Median: 1.63, Measured: true}}
	record, err := recordOverheadCampaign(runs, runtime.GOARCH, "recorded container runtime", "recorded PostgreSQL", runtime.NumCPU())
	if err != nil || record.Attempted != 4 || record.Measured != 3 || record.Maximum != 1.674 || record.Published != 1.7 {
		t.Fatalf("campaign record=%+v err=%v", record, err)
	}
	if _, err := recordOverheadCampaign([]overheadCampaignRun{{}, {}}, "cpu", "runtime", "postgres", 2); err == nil {
		t.Fatal("all-skipped campaign published a ceiling")
	}
}
