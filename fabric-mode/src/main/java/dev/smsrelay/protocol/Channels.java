package dev.smsrelay.protocol;

public final class Channels {
    public static final String CONNECT = "sms:connect";
    public static final String CONNECT_ACK = "sms:connect_ack";
    public static final String DISCONNECT = "sms:disconnect";
    public static final String POSITION = "sms:position";
    public static final String CORRECTION = "sms:correction";
    public static final String BLOCK_CHANGE = "sms:block_change";
    public static final String BLOCK_LOG = "sms:block_log";
    public static final String CHUNK_PRODUCED = "sms:chunk_produced";
    public static final String CHUNK_REQUEST = "sms:chunk_request";
    public static final String CHUNK_RESPONSE = "sms:chunk_response";
    public static final String CHUNK_AUTHORITY = "sms:chunk_authority";
    public static final String ENTITY_UPDATE = "sms:entity_update";
    public static final String ATTACK = "sms:attack";
    public static final String WORLD_STATE = "sms:world_state";
    public static final String PLAYER_LIST = "sms:player_list";
    public static final String ERROR = "sms:error";
    public static final String PING = "sms:ping";
    public static final String PONG = "sms:pong";

    private Channels() {
    }
}
