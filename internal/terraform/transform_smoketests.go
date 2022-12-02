package terraform

import (
	"github.com/hashicorp/terraform/internal/addrs"
	"github.com/hashicorp/terraform/internal/configs"
	"github.com/hashicorp/terraform/internal/dag"
)

// smokeTestTransformer is a GraphTransformer that adds the nodes and edges
// to represent any smoke tests declared in the configuration.
type smokeTestTransformer struct {
	// Config is the root of the configuration tree to add smoke tests from.
	Config *configs.Config
}

var _ GraphTransformer = (*smokeTestTransformer)(nil)

func (t *smokeTestTransformer) Transform(g *Graph) error {
	return t.transformForModule(g, t.Config)
}

func (t *smokeTestTransformer) transformForModule(g *Graph, modCfg *configs.Config) error {
	modAddr := modCfg.Path

	allNodes := g.Vertices()

	for _, st := range modCfg.Module.SmokeTests {
		configAddr := st.Addr().InModule(modAddr)
		preNode := &nodeExpandSmokeTest{
			addr:   configAddr,
			config: st,
			makeInstance: func(addr addrs.AbsSmokeTest, cfg *configs.SmokeTest) dag.Vertex {
				return &nodeSmokeTestPre{
					addr:   addr,
					config: cfg,
				}
			},
		}
		postNode := &nodeExpandSmokeTest{
			addr:   configAddr,
			config: st,
			makeInstance: func(addr addrs.AbsSmokeTest, cfg *configs.SmokeTest) dag.Vertex {
				return &nodeSmokeTestPost{
					addr:   addr,
					config: cfg,
				}
			},
		}
		g.Add(preNode)
		g.Add(postNode)

		// For now we're just making the postNode depend on everything that
		// isn't itself a smoke test expand node, and relying on transitive
		// reduction to eliminate the redundancy. The data blocks inside each
		// smoke test block would've been added earlier by ConfigTransformer
		// and so will be included in allNodes.
		//
		// We also make preNode depend on most things but exclude any
		// data resource nodes that belong to smoke tests, because we must
		// resolve a smoke test's preconditions before resolving its data
		// resources.
		//
		// This is far more conservative than it needs to be, connecting a
		// superset of the actually-needed edges.
		//
		// FIXME: Before stabilizing this, choose a more precise definition
		// of this, because transitive reduction is a very expensive way to
		// tidy this up.
		for _, n := range allNodes {
			g.Connect(dag.BasicEdge(postNode, n))

			if n, isResource := n.(GraphNodeConfigResource); isResource {
				resourceAddr := n.ResourceAddr()
				if !resourceAddr.Module.Equal(modAddr) {
					continue
				}
				resourceCfg := modCfg.Module.ResourceByAddr(resourceAddr.Resource)
				if resourceCfg == nil || resourceCfg.SmokeTest == nil {
					g.Connect(dag.BasicEdge(preNode, n))
				}
			}
		}
		g.Connect(dag.BasicEdge(postNode, preNode))
	}

	for _, child := range modCfg.Children {
		t.transformForModule(g, child)
	}

	return nil
}
