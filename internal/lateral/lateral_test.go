package lateral

import "testing"

func TestPlansUseOpenPorts(t *testing.T) {
	h := Host{Addr: "10.10.10.6", OS: "windows", Ports: []int{445, 5985, 3389}}
	plans := Plans(h)
	if len(plans) != 3 {
		t.Fatalf("want 3 plans, got %d: %+v", len(plans), plans)
	}
	if plans[0].Proto != "winrm" {
		t.Fatalf("winrm should outrank smb/rdp: %+v", plans)
	}
	for _, p := range plans {
		if p.Target != h.Addr {
			t.Fatalf("wrong target: %+v", p)
		}
	}
}

func TestPlanRoutesPrefersShallowPivot(t *testing.T) {
	pivots := []Pivot{
		{OwnedID: 1, Host: "10.0.0.5", CIDRs: []string{"172.16.0.0/16"}, Proxy: "socks5://10.0.0.5:1080"},
		{OwnedID: 2, Host: "10.0.0.9", CIDRs: []string{"172.16.5.0/24"}},
	}
	routes := PlanRoutes([]string{"172.16.5.10", "10.0.0.0/24"}, pivots)
	if len(routes) != 2 {
		t.Fatalf("want 2 routes, got %+v", routes)
	}
	if routes[0].CIDR != "172.16.5.10" || routes[0].Hops == 0 {
		t.Fatalf("closest match should rank first: %+v", routes)
	}
}

func TestPlanRoutesNoPivot(t *testing.T) {
	routes := PlanRoutes([]string{"192.168.1.0/24"}, nil)
	if len(routes) != 1 || routes[0].Hops != 0 {
		t.Fatalf("unreachable target should be marked: %+v", routes)
	}
}
