package main

// tunServiceTakeover records a third-party Windows service we stopped so Sloth can
// bind TUN, and how to restore its SCM start type afterwards.
type tunServiceTakeover struct {
	Name            string
	PrevStartScLine string // e.g. "AUTO_START" or "DEMAND_START" from `sc qc` (empty if unknown)
}
