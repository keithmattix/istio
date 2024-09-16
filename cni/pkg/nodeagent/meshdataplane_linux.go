//go:build linux
// +build linux

package nodeagent

import (
	"context"
	"errors"
	"net/netip"

	"golang.org/x/sys/unix"
	"istio.io/istio/cni/pkg/ipset"
	"istio.io/istio/cni/pkg/iptables"
	"istio.io/istio/cni/pkg/util"
	"istio.io/istio/pkg/util/sets"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

type meshDataplane struct {
	kubeClient         kubernetes.Interface
	netServer          MeshDataplane
	hostIptables       *iptables.IptablesConfigurator
	hostsideProbeIPSet ipset.IPSet
}

func (s *meshDataplane) Start(ctx context.Context) {
	s.netServer.Start(ctx)
}

// addPodToHostNSIpset:
// 1. get pod manifest
// 2. Get all pod ips (might be several, v6/v4)
// 3. update ipsets accordingly
// 4. return the ones we added successfully, and errors for any we couldn't (dupes)
//
// Dupe IPs should be considered an IPAM error and should never happen.
func (s *meshDataplane) addPodToHostNSIpset(pod *corev1.Pod, podIPs []netip.Addr) ([]netip.Addr, error) {
	// Add the pod UID as an ipset entry comment, so we can (more) easily find and delete
	// all relevant entries for a pod later.
	podUID := string(pod.ObjectMeta.UID)
	ipProto := uint8(unix.IPPROTO_TCP)

	var ipsetAddrErrs []error
	var addedIps []netip.Addr

	// For each pod IP
	for _, pip := range podIPs {
		// Add to host ipset
		log.Debugf("adding pod %s probe to ipset %s with ip %s", pod.Name, s.hostsideProbeIPSet.Prefix, pip)
		// Add IP/port combo to set. Note that we set Replace to false here - we _did_ previously
		// set it to true, but in theory that could mask weird scenarios where K8S triggers events out of order ->
		// an add(sameIPreused) then delete(originalIP).
		// Which will result in the new pod starting to fail healthchecks.
		//
		// Since we purge on restart of CNI, and remove pod IPs from the set on every pod removal/deletion,
		// we _shouldn't_ get any overwrite/overlap, unless something is wrong and we are asked to add
		// a pod by an IP we already have in the set (which will give an error, which we want).
		if err := s.hostsideProbeIPSet.AddIP(pip, ipProto, podUID, false); err != nil {
			ipsetAddrErrs = append(ipsetAddrErrs, err)
			log.Errorf("failed adding pod %s to ipset %s with ip %s, error was %s",
				pod.Name, &s.hostsideProbeIPSet.Prefix, pip, err)
		} else {
			addedIps = append(addedIps, pip)
		}
	}

	return addedIps, errors.Join(ipsetAddrErrs...)
}

// createHostsideProbeIpset creates an ipset. This is designed to be called from the host netns.
// Note that if the ipset already exist by name, Create will not return an error.
//
// We will unconditionally flush our set before use here, so it shouldn't matter.
func createHostsideProbeIpset(isV6 bool) (ipset.IPSet, error) {
	linDeps := ipset.RealNlDeps()
	probeSet, err := ipset.NewIPSet(iptables.ProbeIPSet, isV6, linDeps)
	if err != nil {
		return probeSet, err
	}
	probeSet.Flush()
	return probeSet, nil
}

// syncHostIPSets is called after the host node ipset has been created (or found + flushed)
// during initial snapshot creation, it will insert every snapshotted pod's IP into the set.
//
// The set does not allow dupes (obviously, that would be undefined) - but in the real world due to misconfigured
// IPAM or other things, we may see two pods with the same IP on the same node - we will skip the dupes,
// which is all we can do - these pods will fail healthcheck until the IPAM issue is resolved (which seems reasonable)
func (s *meshDataplane) syncHostIPSets(ambientPods []*corev1.Pod) error {
	var addedIPSnapshot []netip.Addr

	for _, pod := range ambientPods {
		podIPs := util.GetPodIPsIfPresent(pod)
		if len(podIPs) == 0 {
			log.Warnf("pod %s does not appear to have any assigned IPs, not syncing with ipset", pod.Name)
		} else {
			addedIps, err := s.addPodToHostNSIpset(pod, podIPs)
			if err != nil {
				log.Errorf("pod %s has IP collision, pod will be skipped and will fail healthchecks", pod.Name, podIPs)
			}
			addedIPSnapshot = append(addedIPSnapshot, addedIps...)
		}

	}
	return pruneHostIPset(sets.New(addedIPSnapshot...), &s.hostsideProbeIPSet)
}

func removePodFromHostNSIpset(pod *corev1.Pod, hostsideProbeSet *ipset.IPSet) error {
	podIPs := util.GetPodIPsIfPresent(pod)
	for _, pip := range podIPs {
		if err := hostsideProbeSet.ClearEntriesWithIP(pip); err != nil {
			return err
		}
		log.Debugf("removed pod name %s with UID %s from host ipset %s by ip %s", pod.Name, pod.UID, hostsideProbeSet.Prefix, pip)
	}

	return nil
}

func pruneHostIPset(expected sets.Set[netip.Addr], hostsideProbeSet *ipset.IPSet) error {
	actualIPSetContents, err := hostsideProbeSet.ListEntriesByIP()
	if err != nil {
		log.Warnf("unable to list IPSet: %v", err)
		return err
	}
	actual := sets.New(actualIPSetContents...)
	stales := actual.DifferenceInPlace(expected)

	for staleIP := range stales {
		if err := hostsideProbeSet.ClearEntriesWithIP(staleIP); err != nil {
			return err
		}
		log.Debugf("removed stale ip %s from host ipset %s", staleIP, hostsideProbeSet.Prefix)
	}
	return nil
}

func (s *meshDataplane) Stop() {
	log.Info("CNI ambient server terminating, cleaning up node net rules")

	log.Debug("removing host iptables rules")
	s.hostIptables.DeleteHostRules()

	log.Debug("destroying host ipset")
	s.hostsideProbeIPSet.Flush()
	if err := s.hostsideProbeIPSet.DestroySet(); err != nil {
		log.Warnf("could not destroy host ipset on shutdown")
	}

	s.netServer.Stop()
}

func (s *meshDataplane) ConstructInitialSnapshot(ambientPods []*corev1.Pod) error {
	if err := s.syncHostIPSets(ambientPods); err != nil {
		log.Errorf("failed to sync host IPset: %v", err)
		return err
	}

	return s.netServer.ConstructInitialSnapshot(ambientPods)
}

func (s *meshDataplane) AddPodToMesh(ctx context.Context, pod *corev1.Pod, podIPs []netip.Addr, netNs string) error {
	var retErr error
	err := s.netServer.AddPodToMesh(ctx, pod, podIPs, netNs)
	if err != nil {
		log.Errorf("failed to add pod to ztunnel: %v", err)
		if !errors.Is(err, ErrPartialAdd) {
			return err
		}
		retErr = err
	}

	log.Debugf("annotating pod %s", pod.Name)
	if err := util.AnnotateEnrolledPod(s.kubeClient, &pod.ObjectMeta); err != nil {
		log.Errorf("failed to annotate pod enrollment: %v", err)
		retErr = err
	}

	// ipset is only relevant for pod healthchecks.
	// therefore, if we had *any* error adding the pod to the mesh
	// do not add the pod to the ipset, so that it will definitely *not* pass healthchecks,
	// and the operator can investigate.
	//
	// This is also important to avoid ipset sync issues if we add the pod ip to the ipset, but
	// enrolling fails because ztunnel (or the pod netns, or whatever) isn't ready yet,
	// and the pod is rescheduled with a new IP. In that case we don't get
	// a removal event, and so would never clean up the old IP that we eagerly-added.
	//
	// TODO one place this *can* fail is
	// - if a CNI plugin after us in the chain fails (currently, we are explicitly the last in the chain by design)
	// - the CmdAdd comes back thru here with a new IP
	// - we will never clean up that old IP that we "lost"
	// To fix this we probably need to impl CmdDel + manage our own podUID/IP mapping.
	if retErr == nil {
		// Handle node healthcheck probe rewrites
		_, err = s.addPodToHostNSIpset(pod, podIPs)
		if err != nil {
			log.Errorf("failed to add pod to ipset: %s/%s %v", pod.Namespace, pod.Name, err)
			return err
		}
	} else {
		log.Errorf("pod: %s/%s was not enrolled and is unhealthy: %v", pod.Namespace, pod.Name, retErr)
	}

	return retErr
}

func (s *meshDataplane) RemovePodFromMesh(ctx context.Context, pod *corev1.Pod, isDelete bool) error {
	log := log.WithLabels("ns", pod.Namespace, "name", pod.Name)

	// Aggregate errors together, so that if part of the removal fails we still proceed with other steps.
	var errs []error

	// Remove the hostside ipset entry first, and unconditionally - if later failures happen, we never
	// want to leave stale entries
	if err := removePodFromHostNSIpset(pod, &s.hostsideProbeIPSet); err != nil {
		log.Errorf("failed to remove pod %s from host ipset, error was: %v", pod.Name, err)
		errs = append(errs, err)
	}

	// Specifically, at this time, we do _not_ want to un-annotate the pod if we cannot successfully remove it,
	// as the pod is probably in a broken state and we don't want to let go of it from the CP perspective while that is true.
	// So we will return if this fails.
	if err := s.netServer.RemovePodFromMesh(ctx, pod, isDelete); err != nil {
		log.Errorf("failed to remove pod from mesh: %v", err)
		errs = append(errs, err)
		return errors.Join(errs...)
	}

	// This should be the last step in all cases - once we do this, the CP will no longer consider this pod "ambient",
	// regardless of the state it is in
	log.Debug("removing annotation from pod")
	if err := util.AnnotateUnenrollPod(s.kubeClient, &pod.ObjectMeta); err != nil {
		log.Errorf("failed to annotate pod unenrollment: %v", err)
		errs = append(errs, err)
		return errors.Join(errs...)
	}

	return errors.Join(errs...)
}
