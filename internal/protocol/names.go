package protocol

import (
	"fmt"
	"math/rand"
	"sync"
	"time"
)

// Adjectives - hacker/cyberpunk/IRC themed
var adjectives = []string{
	// cyber/tech
	"cyber", "neon", "chrome", "quantum", "binary",
	"hex", "crypto", "pixel", "static", "digital",
	"analog", "laser", "plasma", "atomic", "nuclear",
	"sonic", "hyper", "ultra", "mega", "nano",
	"micro", "macro", "proto", "meta", "alpha",
	"beta", "gamma", "delta", "omega", "sigma",
	"theta", "zeta", "apex", "prime", "zero",
	"null", "void", "dark", "bright", "lucid",

	// stealth/mystery
	"ghost", "shadow", "phantom", "stealth", "silent",
	"hidden", "masked", "veiled", "cloaked", "shrouded",
	"cryptic", "arcane", "mystic", "occult", "spectral",
	"ethereal", "astral", "liminal", "twilight", "obsidian",

	// movement/speed
	"swift", "quick", "rapid", "flash", "blur",
	"dash", "rush", "surge", "bolt", "streak",
	"drift", "glide", "flow", "flux", "pulse",
	"wave", "ripple", "tremor", "quake", "rumble",

	// attitude/style
	"rogue", "rebel", "feral", "wild", "lone",
	"grim", "stern", "cold", "cool", "slick",
	"sharp", "keen", "clever", "sly", "wry",
	"bold", "brash", "fierce", "gritty", "raw",

	// glitch/chaos
	"glitch", "corrupt", "broken", "fractured", "jagged",
	"twisted", "warped", "bent", "skewed", "volatile",
	"erratic", "chaotic", "random", "entropy", "noisy",

	// elements
	"iron", "steel", "copper", "silver", "cobalt",
	"carbon", "crystal", "glass", "ember", "ash",
	"frost", "ice", "snow", "storm", "thunder",
	"lightning", "solar", "lunar", "stellar", "cosmic",
}

// Nouns - hacker/IRC/gaming themed
var nouns = []string{
	// animals - predators
	"wolf", "fox", "hawk", "falcon", "eagle",
	"raven", "crow", "owl", "viper", "cobra",
	"python", "tiger", "panther", "jaguar", "lynx",
	"lion", "bear", "shark", "orca", "raptor",
	"dragon", "phoenix", "griffin", "hydra", "wyrm",
	"mantis", "hornet", "wasp", "spider", "scorpion",

	// tech/computing
	"byte", "node", "daemon", "proxy", "socket",
	"cipher", "hash", "packet", "kernel", "root",
	"shell", "port", "gate", "cache", "stack",
	"heap", "queue", "thread", "mutex", "pipe",
	"buffer", "stream", "block", "chunk", "sector",
	"vector", "matrix", "tensor", "array", "graph",
	"codec", "crypt", "vault", "shard", "fragment",

	// weapons/tools
	"blade", "sword", "dagger", "knife", "axe",
	"hammer", "lance", "spear", "arrow", "bolt",
	"cannon", "rifle", "sniper", "turret", "missile",
	"probe", "drone", "beacon", "relay", "antenna",

	// nature/elements
	"storm", "frost", "flame", "spark", "ember",
	"blaze", "inferno", "tsunami", "typhoon", "cyclone",
	"aurora", "nova", "pulsar", "quasar", "nebula",
	"comet", "meteor", "asteroid", "void", "abyss",
	"ridge", "peak", "cliff", "canyon", "ravine",

	// abstract/concepts
	"echo", "signal", "pulse", "wave", "surge",
	"core", "nexus", "axis", "vertex", "origin",
	"zenith", "nadir", "limbo", "wraith", "specter",
	"shade", "gloom", "dusk", "dawn", "haze",
	"mirage", "illusion", "paradox", "enigma", "oracle",
}

var rng = rand.New(rand.NewSource(time.Now().UnixNano()))

// GenerateAgentName creates a random hacker-style nickname.
// Format: adjective_noun (e.g., "ghost_wolf", "neon_daemon")
func GenerateAgentName() string {
	adj := adjectives[rng.Intn(len(adjectives))]
	noun := nouns[rng.Intn(len(nouns))]
	return fmt.Sprintf("%s_%s", adj, noun)
}

// AgentID is a unique identifier for an agent.
// Format: hacker-style nickname (e.g., "ghost_wolf", "neon_daemon")
type AgentID string

// NewAgentID generates a new agent ID.
func NewAgentID() AgentID {
	return AgentID(GenerateAgentName())
}

// NewAgentIDFrom creates an AgentID from a string (for parsing).
func NewAgentIDFrom(s string) AgentID {
	return AgentID(s)
}

// String returns the agent ID as a string.
func (id AgentID) String() string {
	return string(id)
}

// NameGenerator handles unique name generation with collision detection.
// All methods are safe for concurrent use.
type NameGenerator struct {
	mu   sync.Mutex
	used map[string]bool
}

// NewNameGenerator creates a new name generator.
func NewNameGenerator() *NameGenerator {
	return &NameGenerator{
		used: make(map[string]bool),
	}
}

// Generate creates a unique agent ID, retrying on collision.
func (g *NameGenerator) Generate() AgentID {
	g.mu.Lock()
	defer g.mu.Unlock()

	for attempts := 0; attempts < 1000; attempts++ {
		name := GenerateAgentName()
		if !g.used[name] {
			g.used[name] = true
			return AgentID(name)
		}
	}
	// Fallback: add timestamp suffix for guaranteed uniqueness
	name := fmt.Sprintf("%s_%d", GenerateAgentName(), time.Now().UnixNano()%10000)
	g.used[name] = true
	return AgentID(name)
}

// Release marks an agent ID as available for reuse.
func (g *NameGenerator) Release(id AgentID) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.used, string(id))
}

// IsUsed checks if an agent ID is currently in use.
func (g *NameGenerator) IsUsed(id AgentID) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.used[string(id)]
}
