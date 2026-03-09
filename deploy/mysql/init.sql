CREATE DATABASE IF NOT EXISTS go_im DEFAULT CHARSET utf8mb4 COLLATE utf8mb4_unicode_ci;
USE go_im;

-- 用户表
CREATE TABLE users (
    id         BIGINT PRIMARY KEY,                 -- Snowflake ID
    username   VARCHAR(32)  NOT NULL UNIQUE,
    password   VARCHAR(128) NOT NULL,              -- bcrypt hash
    nickname   VARCHAR(64)  NOT NULL DEFAULT '',
    avatar     VARCHAR(256) NOT NULL DEFAULT '',
    status     TINYINT      NOT NULL DEFAULT 1,    -- 1:正常 2:封禁
    created_at TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_username (username)
) ENGINE=InnoDB;

-- 好友关系表
CREATE TABLE friendships (
    id         BIGINT PRIMARY KEY AUTO_INCREMENT,
    user_id    BIGINT   NOT NULL,
    friend_id  BIGINT   NOT NULL,
    status     TINYINT  NOT NULL DEFAULT 0,        -- 0:待确认 1:已接受
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_user_friend (user_id, friend_id),
    INDEX idx_friend_id (friend_id)
) ENGINE=InnoDB;

-- 群组表
CREATE TABLE `groups` (
    id          BIGINT PRIMARY KEY,                -- Snowflake ID
    name        VARCHAR(64) NOT NULL,
    owner_id    BIGINT      NOT NULL,
    avatar      VARCHAR(256) NOT NULL DEFAULT '',
    created_at  TIMESTAMP   NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB;

-- 群成员表
CREATE TABLE group_members (
    id         BIGINT PRIMARY KEY AUTO_INCREMENT,
    group_id   BIGINT  NOT NULL,
    user_id    BIGINT  NOT NULL,
    role       TINYINT NOT NULL DEFAULT 0,         -- 0:普通 1:管理员 2:群主
    joined_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_group_user (group_id, user_id)
) ENGINE=InnoDB;

-- 消息主表
CREATE TABLE messages (
    id           BIGINT PRIMARY KEY,                -- Snowflake ID，自然有序
    msg_id       VARCHAR(64)  NOT NULL UNIQUE,      -- 客户端生成的 UUID，防重幂等
    from_id      BIGINT       NOT NULL,
    to_id        BIGINT       NOT NULL,             -- 接收人ID 或 群组ID
    chat_type    TINYINT      NOT NULL,             -- 1:单聊 2:群聊
    content_type TINYINT      NOT NULL DEFAULT 1,   -- 1:文本 2:图片 3:文件
    content      TEXT         NOT NULL,
    created_at   TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_to_time (to_id, created_at)
) ENGINE=InnoDB;

-- 离线消息表
CREATE TABLE offline_messages (
    id         BIGINT PRIMARY KEY AUTO_INCREMENT,
    user_id    BIGINT  NOT NULL,                    -- 接收方ID
    message_id BIGINT  NOT NULL,                    -- 关联 messages 表的 ID
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_user_id (user_id)
) ENGINE=InnoDB;
