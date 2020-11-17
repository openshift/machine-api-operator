package machinehealthcheck

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	"github.com/openshift/machine-api-operator/pkg/apis/machine/v1beta1"
)

const (
	maxHistory        int = 5
	targetKindMachine     = "Machine"
	targetKindNode        = "Node"
)

func (t *target) missingNodeDetected() {
	klog.Infof("history: missing node detected")

	// if we have an ongoing remediation for this node, it means that the node was deleted and is fenced now
	for i, r := range t.MHC.Status.RemediationHistory {
		if t.matchesTarget(r) && r.Finished == nil {
			if r.Started != nil && r.Fenced == nil {
				now := metav1.Now()
				t.MHC.Status.RemediationHistory[i].Fenced = &now
			}
			return
		}
	}

	// no matching ongoing remediation found, so this is a new problem
	t.unhealthyMachineDetected("missing node")
}

// convenience wrapper around unhealthyConditionDetected for unhealthy machines
func (t *target) unhealthyMachineDetected(reason string) {
	t.unhealthyDetected(nil, nil, reason)
}

func (t *target) unhealthyConditionDetected(cType *v1.NodeConditionType, cStatus *v1.ConditionStatus) {
	t.unhealthyDetected(cType, cStatus, "")
}

func (t *target) unhealthyDetected(cType *v1.NodeConditionType, cStatus *v1.ConditionStatus, reason string) {

	klog.Infof("history: unhealthy detected")

	// check if this is tracked already
	for _, r := range t.MHC.Status.RemediationHistory {
		if t.matchesTarget(r) {
			if r.Finished != nil {
				// this is done, check next
				continue
			}
			if r.Started != nil && r.Finished == nil {
				// this is ongoing (no matter why), no need to track another one
				return
			}
			if t.matchesCondition(r, cType, cStatus) {
				// already tracked
				return
			}
			// TODO what to do here: we are tracking a remediation already for this target, but for another reason...?
			// since it will be hard at a later point to find the right entry, let's just track 1 remediation per target
			return
		}
	}

	// create history if needed
	if t.MHC.Status.RemediationHistory == nil {
		t.MHC.Status.RemediationHistory = make([]v1beta1.Remediation, 0)
	}
	// remove oldest entry if needed
	if len(t.MHC.Status.RemediationHistory) >= maxHistory {
		t.removeOldestRemediation()
	}

	// create new record
	// when we got a node condition, record for the node; otherwise for the machine
	now := metav1.Now()
	r := v1beta1.Remediation{
		Detected: &now,
		Reason:   reason,
	}
	if cType != nil {
		r.ConditionType = cType
		r.ConditionStatus = cStatus
		r.TargetKind = targetKindNode
		r.TargetName = t.Node.Name
	} else {
		r.TargetKind = targetKindMachine
		r.TargetName = t.Machine.Name
	}
	t.MHC.Status.RemediationHistory = append(t.MHC.Status.RemediationHistory, r)

	klog.Infof("history: %+v", t.MHC.Status.RemediationHistory)

}

func (t *target) healthyMachineDetected() {
	t.healthyConditionDetected(nil, nil)
}

func (t *target) healthyConditionDetected(cType *v1.NodeConditionType, cStatus *v1.ConditionStatus) {

	klog.Infof("history: healthy detected")

	// check if this is tracked and not started yet; if so, remove
	for i, r := range t.MHC.Status.RemediationHistory {
		if t.matchesTarget(r) && t.matchesCondition(r, cType, cStatus) {
			if r.Started == nil {
				// this has not started remediation yet, so just remove it
				h := t.MHC.Status.RemediationHistory
				t.MHC.Status.RemediationHistory = append(h[:i], h[i+1:]...)
			}
			if r.Started != nil && r.Finished == nil {
				// this is healthy now
				// that means it was successfully fenced by external remediation without machine deletion
				now := metav1.Now()
				t.MHC.Status.RemediationHistory[i].Finished = &now
			}
		}
	}

	klog.Infof("history: %+v", t.MHC.Status.RemediationHistory)

}

func (t *target) remediationStarted(rType string) {

	klog.Infof("history: remediation started, %s", rType)

	for i, r := range t.MHC.Status.RemediationHistory {
		if t.matchesTarget(r) && r.Started == nil {
			now := metav1.Now()
			t.MHC.Status.RemediationHistory[i].Started = &now
			t.MHC.Status.RemediationHistory[i].Type = rType
		}
	}

	klog.Infof("history: %+v", t.MHC.Status.RemediationHistory)

}

func (t *target) matchesTarget(r v1beta1.Remediation) bool {
	switch r.TargetKind {
	case targetKindMachine:
		if r.TargetName != t.Machine.Name {
			return false
		}
		break
	case targetKindNode:
		if r.TargetName != t.Node.Name {
			return false
		}
		break
	default:
		// we have an unknown kind?!
		return false
	}
	return true
}

func (t *target) matchesCondition(r v1beta1.Remediation, cType *v1.NodeConditionType, cStatus *v1.ConditionStatus) bool {

	switch r.TargetKind {
	case targetKindMachine:
		// no conditions for machines triggering remediation
		return true
	case targetKindNode:
		return cType != nil && cStatus != nil && *r.ConditionType == *cType && *r.ConditionStatus == *cStatus
	default:
		// we have an unknown kind?!
		return false
	}
}

func (t *target) removeOldestRemediation() {
	h := t.MHC.Status.RemediationHistory
	oldestIndex := 0
	for i, r := range h {
		if r.Detected.Before(h[oldestIndex].Detected) {
			oldestIndex = i
		}
	}
	t.MHC.Status.RemediationHistory = append(h[:oldestIndex], h[oldestIndex+1:]...)
}
