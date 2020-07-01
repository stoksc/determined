package searcher

import (
	"github.com/determined-ai/determined/master/pkg/model"
	"github.com/determined-ai/determined/master/pkg/nprand"
)

type context struct {
	rand    *nprand.State
	hparams model.Hyperparameters
}

// SearchMethod is the interface for hyper-parameter tuning methods. Implementations of this
// interface should use pointer receivers to ensure interface equality is calculated through pointer
// equality.
type SearchMethod interface {
	// initialOperations returns a set of initial operations that the searcher would like to take.
	// This should be called only once after the searcher has been created.
	initialOperations(ctx context) ([]Operation, error)
	// trialCreated informs the searcher that a trial has been created as a result of a Create
	// operation.
	trialCreated(ctx context, requestID RequestID) ([]Operation, error)
	// trainCompleted informs the searcher that the training workload initiated by the same searcher
	// has completed. It returns any new operations as a result of this workload completing.
	trainCompleted(ctx context, requestID RequestID, message Workload) ([]Operation, error)
	// checkpointCompleted informs the searcher that the checkpoint workload initiated by the same
	// searcher has completed. It returns any new operations as a result of this workload
	// completing.
	checkpointCompleted(
		ctx context, requestID RequestID, message Workload,
		metrics CheckpointMetrics,
	) ([]Operation, error)
	// validationCompleted informs the searcher that the validation workload initiated by the same
	// searcher has completed. It returns any new operations as a result of this workload
	// completing.
	validationCompleted(
		ctx context, requestID RequestID, message Workload,
		metrics ValidationMetrics,
	) ([]Operation, error)
	// trialClosed informs the searcher that the trial has been closed as a result of a Close
	// operation.
	trialClosed(ctx context, requestID RequestID) ([]Operation, error)
	// progress returns experiment progress as a float between 0.0 and 1.0. As search methods
	// receive completed workloads, they should internally track progress.
	progress() float64
	// trialExitedEarly informs the searcher that the trial has exited earlier than expected.
	trialExitedEarly(ctx context, requestID RequestID, message Workload) ([]Operation, error)
}

// NewSearchMethod returns a new search method for the provided searcher configuration.
func NewSearchMethod(
	c model.SearcherConfig, batchesPerStep, recordsPerEpoch int,
) SearchMethod {
	c = c.Shim(batchesPerStep)
	switch {
	case c.SingleConfig != nil:
		return newSingleSearch(*c.SingleConfig, batchesPerStep, recordsPerEpoch)
	case c.RandomConfig != nil:
		return newRandomSearch(*c.RandomConfig, batchesPerStep, recordsPerEpoch)
	case c.GridConfig != nil:
		return newGridSearch(*c.GridConfig, batchesPerStep, recordsPerEpoch)
	case c.SyncHalvingConfig != nil:
		return newSyncHalvingSearch(*c.SyncHalvingConfig, batchesPerStep, recordsPerEpoch)
	case c.AdaptiveConfig != nil:
		return newAdaptiveSearch(*c.AdaptiveConfig, batchesPerStep, recordsPerEpoch)
	case c.AdaptiveSimpleConfig != nil:
		return newAdaptiveSimpleSearch(*c.AdaptiveSimpleConfig, batchesPerStep, recordsPerEpoch)
	case c.AsyncHalvingConfig != nil:
		return newAsyncHalvingSearch(*c.AsyncHalvingConfig, batchesPerStep, recordsPerEpoch)
	case c.AdaptiveASHAConfig != nil:
		return newAdaptiveASHASearch(*c.AdaptiveASHAConfig, batchesPerStep, recordsPerEpoch)
	case c.PBTConfig != nil:
		return newPBTSearch(*c.PBTConfig, batchesPerStep, recordsPerEpoch)
	default:
		panic("no searcher type specified")
	}
}

type defaultSearchMethod struct{}

func (d *defaultSearchMethod) trialCreated(context, RequestID) ([]Operation, error) {
	return nil, nil
}
func (d *defaultSearchMethod) trainCompleted(context, RequestID, Workload) ([]Operation, error) {
	return nil, nil
}

func (d *defaultSearchMethod) checkpointCompleted(
	context, RequestID, Workload, CheckpointMetrics,
) ([]Operation, error) {
	return nil, nil
}
func (d *defaultSearchMethod) validationCompleted(
	context, RequestID, Workload, ValidationMetrics,
) ([]Operation, error) {
	return nil, nil
}
func (d *defaultSearchMethod) trialClosed(context, RequestID) ([]Operation, error) {
	return nil, nil
}

func (d defaultSearchMethod) trialExitedEarly( //nolint: unused
	ctx context, requestID RequestID, message Workload) ([]Operation, error) {
	return []Operation{Shutdown{Failure: true}}, nil
}
