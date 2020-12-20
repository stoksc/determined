package searcher

import (
	"encoding/json"
	"math"

	"github.com/determined-ai/determined/master/pkg/workload"

	"github.com/pkg/errors"

	"github.com/determined-ai/determined/master/pkg/model"
	"github.com/determined-ai/determined/master/pkg/nprand"
)

type (
	// SearcherState encapsulates the state of the searcher
	SearcherState struct {
		TrialOperations     []Operation
		TrialsRequested     int
		TrialsClosed        int
		TrialIDs            map[model.RequestID]int
		RequestIDs          map[int]model.RequestID
		EarlyExits          map[model.RequestID]bool
		TotalUnitsCompleted float64
		Shutdown            bool

		SearchMethodState json.RawMessage
	}

	// Searcher encompasses the state as the searcher progresses using the provided search method.
	Searcher struct {
		rand    *nprand.State
		hparams model.Hyperparameters
		method  SearchMethod
		SearcherState
	}
)

// NewSearcher creates a new Searcher configured with the provided searcher config.
func NewSearcher(seed uint32, method SearchMethod, hparams model.Hyperparameters) *Searcher {
	return &Searcher{
		rand:    nprand.New(seed),
		hparams: hparams,
		method:  method,
		SearcherState: SearcherState{
			TrialIDs:   map[model.RequestID]int{},
			RequestIDs: map[int]model.RequestID{},
			EarlyExits: map[model.RequestID]bool{},
		},
	}
}

func (s *Searcher) context() context {
	return context{rand: s.rand, hparams: s.hparams}
}

// InitialOperations return a set of initial operations that the searcher would like to take.
// This should be called only once after the searcher has been created.
func (s *Searcher) InitialOperations() ([]Operation, error) {
	operations, err := s.method.initialOperations(s.context())
	if err != nil {
		return nil, errors.Wrap(err, "error while fetching initial operations of search method")
	}
	return operations, nil
}

// TrialCreated informs the searcher that a trial has been created as a result of a Create
// operation.
func (s *Searcher) TrialCreated(create Create, trialID int) ([]Operation, error) {
	s.TrialIDs[create.RequestID] = trialID
	s.RequestIDs[trialID] = create.RequestID
	operations, err := s.method.trialCreated(s.context(), create.RequestID)
	if err != nil {
		return nil, errors.Wrapf(err,
			"error while handling a trial created event: %s", create.RequestID)
	}
	s.Record(operations)
	return operations, nil
}

// TrialExitedEarly indicates to the searcher that the trial with the given trialID exited early.
func (s *Searcher) TrialExitedEarly(
	trialID int, exitedReason *workload.ExitedReason,
) ([]Operation, error) {
	requestID, ok := s.RequestIDs[trialID]
	if !ok {
		return nil, errors.Errorf("unexpected trial ID sent to searcher: %d", trialID)
	}

	// If we are exiting early and we haven't seen this exiting early
	// message before, return true to send the message down to the search
	// method.
	if _, ok := s.EarlyExits[requestID]; !ok {
		s.EarlyExits[requestID] = true
	}
	operations, err := s.method.trialExitedEarly(s.context(), requestID, *exitedReason)
	if err != nil {
		return nil, errors.Wrapf(err, "error relaying trial exited early to trial %d", trialID)
	}
	s.Record(operations)
	return operations, nil
}

// WorkloadCompleted informs the searcher that the workload is completed. This relays the message
// to the event log and records the units as complete for search method progress.
func (s *Searcher) WorkloadCompleted(unitsCompleted float64) {
	s.TotalUnitsCompleted += unitsCompleted
}

// OperationCompleted informs the searcher that the given workload initiated by the same searcher
// has completed. Returns any new operations as a result of this workload completing.
func (s *Searcher) OperationCompleted(
	trialID int, op Runnable, metrics interface{},
) ([]Operation, error) {
	requestID, ok := s.RequestIDs[trialID]
	if !ok {
		return nil, errors.Errorf("unexpected trial ID sent to searcher: %d", trialID)
	}

	var operations []Operation
	var err error

	switch tOp := op.(type) {
	case Train:
		operations, err = s.method.trainCompleted(s.context(), requestID, tOp)
	case Checkpoint:
		operations, err = s.method.checkpointCompleted(
			s.context(), requestID, tOp, *metrics.(*workload.CheckpointMetrics))
	case Validate:
		operations, err = s.method.validationCompleted(
			s.context(), requestID, tOp, *metrics.(*workload.ValidationMetrics))
	default:
		return nil, errors.Errorf("unexpected op: %s", tOp)
	}

	if err != nil {
		return nil, errors.Wrapf(err, "error while handling a workload completed event: %s", requestID)
	}
	s.Record(operations)
	return operations, nil
}

// TrialClosed informs the searcher that the trial has been closed as a result of a Close operation.
func (s *Searcher) TrialClosed(requestID model.RequestID) ([]Operation, error) {
	s.TrialsClosed++
	operations, err := s.method.trialClosed(s.context(), requestID)
	if err != nil {
		return nil, errors.Wrapf(err, "error while handling a trial closed event: %s", requestID)
	}
	s.Record(operations)
	if s.TrialsRequested == s.TrialsClosed {
		shutdown := Shutdown{Failure: len(s.EarlyExits) >= s.TrialsRequested}
		operations = append(operations, shutdown)
	}
	return operations, nil
}

// Progress returns experiment progress as a float between 0.0 and 1.0.
func (s *Searcher) Progress() float64 {
	progress := s.method.progress(s.TotalUnitsCompleted)
	if math.IsNaN(progress) || math.IsInf(progress, 0) {
		return 0.0
	}
	return progress
}

// TrialID finds the trial ID for the provided request ID. The first return value is the trial ID
// if the trial has been created; otherwise, it is 0. The second is whether or not the trial has
// been created.
func (s *Searcher) TrialID(id model.RequestID) (int, bool) {
	trialID, ok := s.TrialIDs[id]
	return trialID, ok
}

// RequestID finds the request ID for the provided trial ID. The first return value is the request
// ID if the trial has been created; otherwise, it is undefined. The second is whether or not the
// trial has been created.
func (s *Searcher) RequestID(id int) (model.RequestID, bool) {
	requestID, ok := s.RequestIDs[id]
	return requestID, ok
}

// Record records operations that were requested by the searcher for a specific trial.
func (s *Searcher) Record(ops []Operation) {
	for _, op := range ops {
		s.TrialOperations = append(s.TrialOperations, op)
		switch op.(type) {
		case Create:
			s.TrialsRequested++
		case Shutdown:
			s.Shutdown = true
		}
	}
}

// Save returns a searchers current state.
func (s *Searcher) Save() (json.RawMessage, error) {
	b, err := s.method.save()
	if err != nil {
		return nil, errors.Wrap(err, "failed to save search method")
	}
	s.SearcherState.SearchMethodState = b
	return json.Marshal(s.SearcherState)
}

// Load loads a searcher from prior state.
func (s *Searcher) Load(state json.RawMessage, exp model.Experiment) error {
	s.rand.Seed(exp.Config.Reproducibility.ExperimentSeed)
	s.hparams = exp.Config.Hyperparameters
	if state == nil {
		return nil
	}
	if err := json.Unmarshal(state, &s.SearcherState); err != nil {
		return errors.Wrap(err, "failed to unmarshal searcher snapshot")
	}
	return s.method.load(s.SearchMethodState)
}
