package smartcrusher

// SmartCrusherBuilder provides fluent API for building SmartCrusher.
type SmartCrusherBuilder struct {
	config      SmartCrusherConfig
	constraints []Constraint
	observers   []Observer
}

// NewSmartCrusherBuilder creates an empty builder.
func NewSmartCrusherBuilder(config SmartCrusherConfig) *SmartCrusherBuilder {
	return &SmartCrusherBuilder{
		config: config,
	}
}

// AddConstraint appends a constraint.
func (b *SmartCrusherBuilder) AddConstraint(c Constraint) *SmartCrusherBuilder {
	b.constraints = append(b.constraints, c)
	return b
}

// AddDefaultOSSConstraints appends the OSS default constraint stack.
func (b *SmartCrusherBuilder) AddDefaultOSSConstraints() *SmartCrusherBuilder {
	b.constraints = append(b.constraints, DefaultOSSConstraints()...)
	return b
}

// AddObserver appends an observer.
func (b *SmartCrusherBuilder) AddObserver(o Observer) *SmartCrusherBuilder {
	b.observers = append(b.observers, o)
	return b
}

// WithDefaultOSSSetup applies default scorer, constraints, and observer.
func (b *SmartCrusherBuilder) WithDefaultOSSSetup() *SmartCrusherBuilder {
	return b.AddDefaultOSSConstraints().AddObserver(TracingObserver{})
}

// Build constructs the SmartCrusher.
func (b *SmartCrusherBuilder) Build() *SmartCrusher {
	return &SmartCrusher{
		Config:      b.config,
		Analyzer:    NewSmartAnalyzer(b.config),
		Constraints: b.constraints,
		Observers:   b.observers,
	}
}
