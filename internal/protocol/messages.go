package protocol

const (
	ChannelConnect        = "sms:connect"
	ChannelConnectAck     = "sms:connect_ack"
	ChannelDisconnect     = "sms:disconnect"
	ChannelPosition       = "sms:position"
	ChannelCorrection     = "sms:correction"
	ChannelBlockChange    = "sms:block_change"
	ChannelBlockLog       = "sms:block_log"
	ChannelChunkProduced  = "sms:chunk_produced"
	ChannelChunkRequest   = "sms:chunk_request"
	ChannelChunkResponse  = "sms:chunk_response"
	ChannelChunkAuthority = "sms:chunk_authority"
	ChannelEntityUpdate   = "sms:entity_update"
	ChannelAttack         = "sms:attack"
	ChannelWorldState     = "sms:world_state"
	ChannelPlayerList     = "sms:player_list"
	ChannelError          = "sms:error"
	ChannelPing           = "sms:ping"
	ChannelPong           = "sms:pong"
)

type ConnectPayload struct {
	UUID     string `json:"uuid"`
	Username string `json:"username"`
	GameMode string `json:"game_mode"`
	World    string `json:"world"`
}

type ConnectAckPayload struct {
	PlayerID       string `json:"player_id"`
	Username       string `json:"username"`
	ServerTime     int64  `json:"server_time"`
	PingIntervalMS int64  `json:"ping_interval_ms"`
}

type PlayerInfo struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	GameMode string `json:"game_mode"`
	World    string `json:"world"`
	PingMS   int64  `json:"ping_ms"`
}

type PlayerListPayload struct {
	Players    []PlayerInfo `json:"players"`
	ServerTime int64        `json:"server_time"`
}

type PositionUpdatePayload struct {
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	Z        float64 `json:"z"`
	Yaw      float64 `json:"yaw"`
	Pitch    float64 `json:"pitch"`
	OnGround bool    `json:"on_ground"`
}

type PositionStatePayload struct {
	PlayerID   string  `json:"player_id"`
	Username   string  `json:"username"`
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	Z          float64 `json:"z"`
	Yaw        float64 `json:"yaw"`
	Pitch      float64 `json:"pitch"`
	OnGround   bool    `json:"on_ground"`
	ServerTime int64   `json:"server_time"`
}

type CorrectionPayload struct {
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	Z          float64 `json:"z"`
	Yaw        float64 `json:"yaw"`
	Pitch      float64 `json:"pitch"`
	Reason     string  `json:"reason"`
	ServerTime int64   `json:"server_time"`
}

type BlockChangePayload struct {
	World   string `json:"world"`
	X       int    `json:"x"`
	Y       int    `json:"y"`
	Z       int    `json:"z"`
	BlockID string `json:"block_id"`
}

type BlockChangeEvent struct {
	Sequence   uint64 `json:"sequence"`
	ActorID    string `json:"actor_id"`
	ActorName  string `json:"actor_name"`
	World      string `json:"world"`
	X          int    `json:"x"`
	Y          int    `json:"y"`
	Z          int    `json:"z"`
	BlockID    string `json:"block_id"`
	ServerTime int64  `json:"server_time"`
}

type BlockLogPayload struct {
	Events []BlockChangeEvent `json:"events"`
}

type ChunkProducedPayload struct {
	ChunkID string `json:"chunk_id"`
	Summary []byte `json:"summary,omitempty"`
}

const (
	ChunkStatusAccepted = "accepted"
	ChunkStatusConflict = "conflict"
)

type ChunkAuthorityPayload struct {
	ChunkID    string `json:"chunk_id"`
	OwnerID    string `json:"owner_id"`
	OwnerName  string `json:"owner_name"`
	Summary    []byte `json:"summary,omitempty"`
	Status     string `json:"status"`
	ServerTime int64  `json:"server_time"`
}

type ChunkRequestPayload struct {
	ChunkID string `json:"chunk_id"`
}

type ChunkRequestRelayPayload struct {
	ChunkID       string `json:"chunk_id"`
	RequesterID   string `json:"requester_id"`
	RequesterName string `json:"requester_name"`
	ServerTime    int64  `json:"server_time"`
}

type ChunkResponsePayload struct {
	ChunkID string `json:"chunk_id"`
	Data    []byte `json:"data"`
}

type ChunkResponseRelayPayload struct {
	ChunkID    string `json:"chunk_id"`
	Data       []byte `json:"data"`
	FromID     string `json:"from_id"`
	FromName   string `json:"from_name"`
	ServerTime int64  `json:"server_time"`
}

type EntityUpdatePayload struct {
	EntityID string  `json:"entity_id"`
	Type     string  `json:"type"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	Z        float64 `json:"z"`
}

type EntityStatePayload struct {
	EntityID      string  `json:"entity_id"`
	Type          string  `json:"type"`
	X             float64 `json:"x"`
	Y             float64 `json:"y"`
	Z             float64 `json:"z"`
	AuthorityID   string  `json:"authority_id"`
	AuthorityName string  `json:"authority_name"`
	ServerTime    int64   `json:"server_time"`
}

type AttackPayload struct {
	TargetEntityID string  `json:"target_entity_id,omitempty"`
	X              float64 `json:"x"`
	Y              float64 `json:"y"`
	Z              float64 `json:"z"`
}

type AttackEventPayload struct {
	ActorID        string  `json:"actor_id"`
	ActorName      string  `json:"actor_name"`
	TargetEntityID string  `json:"target_entity_id,omitempty"`
	X              float64 `json:"x"`
	Y              float64 `json:"y"`
	Z              float64 `json:"z"`
	ServerTime     int64   `json:"server_time"`
}

type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Channel string `json:"channel,omitempty"`
}

type PingPayload struct {
	Time int64 `json:"time"`
}

type DisconnectPayload struct {
	Reason string `json:"reason,omitempty"`
}

type WorldStatePayload struct {
	TimeOfDay      int64   `json:"time_of_day"`
	Weather        string  `json:"weather"`
	SpawnX         float64 `json:"spawn_x"`
	SpawnY         float64 `json:"spawn_y"`
	SpawnZ         float64 `json:"spawn_z"`
	BorderDiameter float64 `json:"border_diameter"`
	ServerTime     int64   `json:"server_time"`
}
