package mutation

import (
	"sort"

	"github.com/elastic/stack-operators/stack-operator/pkg/controller/elasticsearch/support"
	corev1 "k8s.io/api/core/v1"
)

// PodComparisonResult holds information about pod comparison result
type PodComparisonResult struct {
	IsMatch               bool
	MatchingPod           corev1.Pod
	MismatchReasonsPerPod map[string][]string
	RemainingPods         []corev1.Pod
}

// CalculateChanges returns Changes to perform by comparing actual pods to expected pods spec
func CalculateChanges(expectedPodSpecCtxs []support.PodSpecContext, state support.ResourcesState, podBuilder PodBuilder) (Changes, error) {
	// work on copies of the arrays, on which we can safely remove elements
	expectedCopy := make([]support.PodSpecContext, len(expectedPodSpecCtxs))
	copy(expectedCopy, expectedPodSpecCtxs)
	actualCopy := make([]corev1.Pod, len(state.CurrentPods))
	copy(actualCopy, state.CurrentPods)

	return mutableCalculateChanges(expectedCopy, actualCopy, state, podBuilder)
}

func mutableCalculateChanges(
	expectedPodSpecCtxs []support.PodSpecContext,
	actualPods []corev1.Pod,
	state support.ResourcesState,
	podBuilder PodBuilder,
) (Changes, error) {
	changes := EmptyChanges()

	for _, expectedPodSpecCtx := range expectedPodSpecCtxs {
		comparisonResult, err := getAndRemoveMatchingPod(expectedPodSpecCtx, actualPods, state)
		if err != nil {
			return changes, err
		}
		if comparisonResult.IsMatch {
			// matching pod already exists, keep it
			changes.ToKeep = append(changes.ToKeep, comparisonResult.MatchingPod)
			// one less pod to compare with
			actualPods = comparisonResult.RemainingPods
		} else {
			// no matching pod, a new one should be added
			pod, err := podBuilder(expectedPodSpecCtx)
			if err != nil {
				return changes, err
			}
			changes.ToAdd = append(changes.ToAdd, PodToAdd{
				Pod:             pod,
				PodSpecCtx:      expectedPodSpecCtx,
				MismatchReasons: comparisonResult.MismatchReasonsPerPod,
			})
		}
	}
	// remaining actual pods should be removed
	changes.ToDelete = actualPods

	// sort changes for idempotent processing
	sort.SliceStable(changes.ToKeep, sortPodByCreationTimestampAsc(changes.ToKeep))
	sort.SliceStable(changes.ToDelete, sortPodByCreationTimestampAsc(changes.ToDelete))

	return changes, nil
}

func getAndRemoveMatchingPod(podSpecCtx support.PodSpecContext, pods []corev1.Pod, state support.ResourcesState) (PodComparisonResult, error) {
	mismatchReasonsPerPod := map[string][]string{}

	for i, pod := range pods {
		if IsTainted(pod) {
			mismatchReasonsPerPod[pod.Name] = []string{TaintedReason}
			continue
		}

		isMatch, mismatchReasons, err := podMatchesSpec(pod, podSpecCtx, state)
		if err != nil {
			return PodComparisonResult{}, err
		}
		if isMatch {
			// matching pod found
			// remove it from the remaining pods
			return PodComparisonResult{
				IsMatch:               true,
				MatchingPod:           pod,
				MismatchReasonsPerPod: mismatchReasonsPerPod,
				RemainingPods:         append(pods[:i], pods[i+1:]...),
			}, nil
		}
		mismatchReasonsPerPod[pod.Name] = mismatchReasons
	}
	// no matching pod found
	return PodComparisonResult{
		IsMatch:               false,
		MismatchReasonsPerPod: mismatchReasonsPerPod,
		RemainingPods:         pods,
	}, nil
}
