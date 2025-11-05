# TODO List

## Config Serialization Support

### Priority: Medium

**Task**: Add JSON serialization support for Config structure, including RequestGenerator and ScheduleGenerator configurations.

**Current Status**: Config structures are hardcoded in source code (`soc_configs.go`). RequestGenerator and ScheduleGenerator are created in Simulator initialization.

**Requirements**:
1. Support JSON serialization/deserialization of Config structure
2. Serialize RequestGenerator configuration:
   - ProbabilityGenerator: `RequestRateConfig` and `SlaveWeights`
   - ScheduleGenerator: `ScheduleConfig` (map[int]map[int][]ScheduleItem)
3. Support loading Config from JSON files
4. Support saving Config to JSON files
5. Ensure backward compatibility with existing hardcoded configs

**Implementation Notes**:
- RequestGenerator interface cannot be directly serialized (interface type)
- Need to introduce a serializable config format that can be converted to Generator
- Consider using a discriminator field to identify generator type
- ScheduleConfig structure is already serializable (but may need custom JSON tags)

**Related Files**:
- `models.go`: Config structure
- `request_generator.go`: RequestGenerator interface and implementations
- `soc_configs.go`: Predefined configs
- `web_server.go`: Config loading/saving endpoints (if needed)

**Estimated Effort**: Medium (2-3 days)

