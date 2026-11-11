# SIKAT — Entity Relationship Diagram

> Status: **PHASE 0.** Diagram menampilkan kolom kunci & relasi. Kolom lengkap,
> constraint, dan index ada di [`schema-draft.sql`](schema-draft.sql); penjelasan
> di [`DATABASE.md`](DATABASE.md).
>
> Diagram dipecah per domain agar terbaca. Tabel yang muncul di lebih dari satu
> diagram adalah tabel yang sama.

---

## 1. Peta Domain

```mermaid
flowchart LR
    ORG["Organisasi<br/>roles, users, departments,<br/>rooms, user_competencies"]
    CAT["Katalog<br/>it_categories, ticket_categories,<br/>action_templates, pending_reasons,<br/>activity_types"]
    AST["Perangkat & QR<br/>asset_categories, assets, qr_codes"]
    TKT["Tiket<br/>tickets, ticket_assignments,<br/>ticket_histories, ticket_comments,<br/>attachments, number_sequences"]
    SLA["Prioritas & SLA<br/>priority_rules, sla_policies,<br/>sla_calendars, sla_calendar_hours,<br/>sla_holidays, sla_escalations"]
    MNT["Pemeliharaan<br/>asset_maintenance_histories,<br/>spare_parts, maintenance_parts,<br/>asset_alerts"]
    ACT["Kegiatan Internal<br/>internal_activities, activity_members"]
    NTF["Notifikasi<br/>outbox_events, notifications,<br/>push_subscriptions,<br/>notification_preferences"]
    INT["Integrasi<br/>integration_configs,<br/>integration_sync_logs,<br/>external_id_mappings"]
    SYS["Sistem<br/>audit_logs, system_settings,<br/>sessions, backup_logs"]

    ORG --> AST
    ORG --> TKT
    CAT --> TKT
    AST --> TKT
    SLA --> TKT
    TKT --> MNT
    AST --> MNT
    ACT --> MNT
    TKT --> NTF
    SLA --> NTF
    MNT --> NTF
    INT -.pemetaan external_id.-> ORG
    INT -.pemetaan external_id.-> AST
```

---

## 2. Organisasi, Katalog, Perangkat & QR

```mermaid
erDiagram
    roles ||--o{ users : "memiliki"
    departments ||--o{ users : "unit kerja"
    departments ||--o{ rooms : "memiliki"
    users ||--o{ user_competencies : "kompeten di"
    it_categories ||--o{ user_competencies : ""
    it_categories ||--o{ ticket_categories : "mengelompokkan"
    it_categories |o--o{ asset_categories : "bidang default"
    it_categories |o--o{ activity_types : "bidang default"
    it_categories |o--o{ action_templates : ""
    ticket_categories |o--o{ action_templates : ""
    asset_categories ||--o{ assets : "mengelompokkan"
    departments |o--o{ assets : "milik unit"
    rooms |o--o{ assets : "berada di"
    rooms |o--o{ qr_codes : "QR ruangan"
    assets |o--o{ qr_codes : "QR perangkat"
    qr_codes |o--o| qr_codes : "diganti oleh"

    roles {
        bigint id PK
        varchar code UK "ADMIN, COORDINATOR, TECHNICIAN"
        varchar name
    }
    users {
        bigint id PK
        bigint role_id FK
        bigint department_id FK
        varchar username UK
        varchar full_name
        varchar nip UK
        varchar password_hash "argon2id"
        boolean is_active
        enum source "LOCAL SIMRS CSV"
    }
    departments {
        bigint id PK
        varchar code UK
        varchar name UK
        boolean is_critical_service
        boolean is_active
    }
    rooms {
        bigint id PK
        bigint department_id FK
        varchar code UK
        varchar name
        boolean is_critical_service "NULL ikut unit"
    }
    user_competencies {
        bigint user_id PK, FK
        bigint it_category_id PK, FK
    }
    it_categories {
        bigint id PK
        varchar code UK
        varchar name UK "Bidang"
    }
    ticket_categories {
        bigint id PK
        bigint it_category_id FK
        varchar name UK "Jenis gangguan"
        enum default_priority
    }
    action_templates {
        bigint id PK
        bigint it_category_id FK
        bigint ticket_category_id FK
        varchar title
        text body
    }
    activity_types {
        bigint id PK
        varchar name UK
        bigint it_category_id FK
    }
    asset_categories {
        bigint id PK
        varchar code UK
        varchar name UK
        bigint it_category_id FK
    }
    assets {
        bigint id PK
        varchar asset_code UK
        varchar name
        bigint asset_category_id FK
        varchar brand
        varchar model
        varchar serial_number
        bigint department_id FK
        bigint room_id FK
        date procured_at
        enum status "ACTIVE BROKEN IN_REPAIR UNUSED DISPOSED"
    }
    qr_codes {
        bigint id PK
        char token UK "22 char random"
        enum type "ROOM ASSET"
        bigint room_id FK
        bigint asset_id FK
        boolean is_active
        int generation
        bigint replaced_by_id FK
        bigint active_room_id UK "generated"
        bigint active_asset_id UK "generated"
    }
```

---

## 3. Tiket, Prioritas & SLA

```mermaid
erDiagram
    qr_codes |o--o{ tickets : "dipindai untuk"
    departments |o--o{ tickets : "lokasi"
    rooms |o--o{ tickets : "lokasi"
    assets |o--o{ tickets : "perangkat bermasalah"
    ticket_categories ||--o{ tickets : "jenis gangguan"
    it_categories ||--o{ tickets : "bidang snapshot"
    priority_rules |o--o{ tickets : "menentukan prioritas"
    sla_policies |o--o{ tickets : "target SLA"
    sla_calendars |o--o{ tickets : "kalender snapshot"
    pending_reasons |o--o{ tickets : "alasan tunda"
    users |o--o{ tickets : "PJ utama aktif"
    tickets ||--o{ ticket_assignments : "penugasan"
    users ||--o{ ticket_assignments : "ditugaskan"
    tickets ||--o{ ticket_histories : "histori"
    users |o--o{ ticket_histories : "pelaksana"
    tickets ||--o{ ticket_comments : "komentar"
    tickets ||--o{ attachments : "foto"
    tickets ||--o{ sla_escalations : "eskalasi"
    sla_calendars ||--o{ sla_calendar_hours : "jam kerja"
    sla_calendars ||--o{ sla_holidays : "hari libur"
    sla_calendars ||--o{ sla_policies : "dipakai"

    tickets {
        bigint id PK
        varchar ticket_no UK "TKT-2026-000001"
        binary public_token_hash UK "sha256"
        enum channel "QR PHONE WALK_IN MESSAGE"
        bigint qr_code_id FK
        bigint department_id FK
        bigint room_id FK
        bigint asset_id FK
        varchar reporter_name
        bigint ticket_category_id FK
        bigint it_category_id FK
        text description
        tinyint impact_level "0-2"
        tinyint affected_scope "1-4"
        enum replacement_available
        boolean critical_service "snapshot"
        enum priority "LOW MEDIUM HIGH CRITICAL"
        enum priority_source "AUTO MANUAL"
        bigint priority_rule_id FK
        enum status "NEW..CANCELLED"
        bigint assignee_id FK
        bigint pending_reason_id FK
        bigint sla_policy_id FK
        int sla_response_minutes "snapshot"
        int sla_resolution_minutes "snapshot"
        int sla_cycle
        int sla_paused_minutes
        datetime response_due_at
        datetime resolution_due_at
        datetime first_response_at
        datetime resolved_at
        text resolution_action
        enum resolution_result
        int version
    }
    ticket_assignments {
        bigint id PK
        bigint ticket_id FK
        bigint user_id FK
        enum role "PRIMARY SUPPORT"
        datetime assigned_at
        datetime released_at
        bigint active_primary_ticket_id UK "generated"
        varchar active_member_key UK "generated"
    }
    ticket_histories {
        bigint id PK
        bigint ticket_id FK
        varchar event_type
        varchar from_status
        varchar to_status
        varchar from_priority
        varchar to_priority
        enum actor_type "USER REPORTER SYSTEM"
        bigint actor_user_id FK
        text note
        json meta
        datetime created_at
    }
    ticket_comments {
        bigint id PK
        bigint ticket_id FK
        enum author_type
        bigint author_user_id FK
        text body
        boolean is_internal
    }
    attachments {
        bigint id PK
        bigint ticket_id FK
        bigint activity_id FK
        bigint maintenance_history_id FK
        varchar storage_key UK
        varchar mime_type
        int size_bytes
        binary sha256
    }
    priority_rules {
        bigint id PK
        int sort_order
        bigint ticket_category_id FK
        bigint it_category_id FK
        bigint department_id FK
        tinyint min_impact
        tinyint min_scope
        tinyint max_scope
        set replacement_in
        boolean critical_service
        enum result_priority
    }
    sla_policies {
        bigint id PK
        enum priority
        bigint ticket_category_id FK
        bigint it_category_id FK
        bigint department_id FK
        bigint calendar_id FK
        int response_minutes
        int resolution_minutes
        tinyint warn_pct
        boolean allow_pause
        varchar match_key UK "generated"
    }
    sla_calendars {
        bigint id PK
        varchar name UK
        varchar timezone "Asia/Makassar"
        boolean is_24x7
        boolean is_default
    }
    sla_calendar_hours {
        bigint id PK
        bigint calendar_id FK
        tinyint weekday "1-7"
        time start_time
        time end_time
    }
    sla_holidays {
        bigint id PK
        bigint calendar_id FK
        date holiday_date
        varchar name
    }
    sla_escalations {
        bigint id PK
        bigint ticket_id FK
        enum kind
        int cycle
        tinyint level
        datetime triggered_at
        bigint outbox_event_id FK
    }
    pending_reasons {
        bigint id PK
        varchar code UK
        varchar label
        boolean pauses_sla
    }
```

---

## 4. Pemeliharaan Perangkat & Kegiatan Internal

```mermaid
erDiagram
    assets ||--o{ asset_maintenance_histories : "riwayat"
    tickets |o--o{ asset_maintenance_histories : "dari penyelesaian tiket"
    internal_activities |o--o{ asset_maintenance_histories : "dari kegiatan"
    asset_alerts |o--o{ asset_maintenance_histories : "keputusan alert"
    ticket_categories |o--o{ asset_maintenance_histories : "jenis kerusakan"
    users |o--o{ asset_maintenance_histories : "petugas"
    asset_maintenance_histories ||--o{ maintenance_parts : "memakai"
    spare_parts ||--o{ maintenance_parts : "dipakai di"
    asset_maintenance_histories ||--o{ attachments : "foto"
    assets ||--o{ asset_alerts : "peringatan"
    tickets |o--o{ asset_alerts : "tiket pemicu terakhir"
    activity_types ||--o{ internal_activities : "jenis"
    it_categories ||--o{ internal_activities : "bidang"
    departments |o--o{ internal_activities : "unit"
    rooms |o--o{ internal_activities : "ruangan"
    assets |o--o{ internal_activities : "perangkat"
    tickets |o--o{ internal_activities : "terkait tiket"
    internal_activities ||--o{ activity_members : "anggota"
    users ||--o{ activity_members : "pelaksana / pendukung"
    internal_activities ||--o{ attachments : "lampiran"

    asset_maintenance_histories {
        bigint id PK
        bigint asset_id FK
        enum maintenance_type "CORRECTIVE PREVENTIVE INSPECTION INSTALLATION RELOCATION ALERT_DECISION"
        enum source "TICKET ACTIVITY MANUAL ALERT"
        bigint ticket_id FK
        int ticket_cycle
        bigint activity_id FK
        bigint asset_alert_id FK
        bigint ticket_category_id FK
        bigint performed_by_user_id FK
        text action_taken
        enum result
        datetime problem_started_at
        datetime started_at
        datetime finished_at
        int repair_minutes "generated"
        int downtime_minutes "generated"
        decimal service_cost
        varchar ticket_cycle_key UK "generated"
    }
    spare_parts {
        bigint id PK
        varchar code UK
        varchar name
        varchar unit
        decimal default_unit_cost
    }
    maintenance_parts {
        bigint id PK
        bigint maintenance_history_id FK
        bigint spare_part_id FK
        decimal quantity
        decimal unit_cost
        decimal line_cost "generated"
    }
    asset_alerts {
        bigint id PK
        bigint asset_id FK
        enum status "OPEN ACKNOWLEDGED CLOSED"
        int ticket_count
        int window_days
        int threshold
        bigint last_ticket_id FK
        enum recommendation "EVALUATE INSPECT CONSIDER_REPLACEMENT"
        enum decision
        bigint decided_by FK
        bigint open_asset_id UK "generated"
    }
    internal_activities {
        bigint id PK
        varchar activity_no UK "KEG-2026-000001"
        date activity_date
        bigint activity_type_id FK
        bigint it_category_id FK
        bigint department_id FK
        bigint room_id FK
        bigint asset_id FK
        bigint related_ticket_id FK
        varchar title
        text result
        enum status
        datetime started_at
        datetime ended_at
        int duration_minutes "generated"
        bigint created_by FK
    }
    activity_members {
        bigint activity_id PK, FK
        bigint user_id PK, FK
        enum role "LEAD SUPPORT"
        bigint lead_activity_id UK "generated"
    }
```

---

## 5. Notifikasi, Integrasi & Sistem

```mermaid
erDiagram
    outbox_events ||--o{ notifications : "menghasilkan"
    outbox_events |o--o{ sla_escalations : "dikirim lewat"
    users ||--o{ notifications : "inbox"
    users ||--o{ push_subscriptions : "perangkat"
    roles ||--o{ notification_preferences : "aturan"
    integration_configs |o--o{ integration_sync_logs : "riwayat sync"
    users |o--o{ integration_sync_logs : "dipicu manual"
    users |o--o{ audit_logs : "pelaku"
    users |o--o{ system_settings : "diubah oleh"

    outbox_events {
        bigint id PK
        varchar event_type
        varchar aggregate_type
        bigint aggregate_id
        json payload
        varchar dedupe_key UK
        enum status "PENDING PROCESSING DONE FAILED"
        int attempts
        datetime next_attempt_at
    }
    notifications {
        bigint id PK
        bigint user_id FK
        bigint event_id FK
        varchar type
        varchar title
        varchar body
        enum severity
        enum push_status
        datetime read_at
    }
    push_subscriptions {
        bigint id PK
        bigint user_id FK
        text endpoint
        binary endpoint_hash UK
        varchar p256dh
        varchar auth_secret
        datetime disabled_at
    }
    notification_preferences {
        bigint id PK
        bigint role_id FK
        varchar event_type
        enum scope "ALL ASSIGNED COMPETENCY"
        boolean inbox
        boolean push
    }
    integration_configs {
        bigint id PK
        enum entity_type UK "DEPARTMENT ROOM EMPLOYEE ASSET"
        enum provider "LOCAL CSV SIMRS_API SIMRS_DB"
        boolean is_enabled
        int schedule_minutes
        json settings
        varbinary secret_ciphertext
        enum conflict_policy
    }
    integration_sync_logs {
        bigint id PK
        bigint config_id FK
        enum entity_type
        enum trigger_type
        enum status
        int inserted
        int updated
        int conflicts
        int failed
        text error_message
    }
    external_id_mappings {
        bigint id PK
        varchar source
        enum entity_type
        varchar external_id
        bigint local_id
        binary source_hash
    }
    audit_logs {
        bigint id PK
        enum actor_type
        bigint actor_user_id FK
        varchar action
        varchar entity_type
        bigint entity_id
        json before_data
        json after_data
        datetime created_at
    }
    system_settings {
        varchar setting_key PK
        text value
        enum value_type
        bigint updated_by FK
    }
    sessions {
        char token PK
        blob data
        timestamp expiry
    }
    backup_logs {
        bigint id PK
        enum status
        datetime started_at
        varchar file_name
        bigint size_bytes
        datetime restore_tested_at
    }
```

> `external_id_mappings.local_id` sengaja **tanpa FK** karena menunjuk ke
> beberapa tabel (sesuai `entity_type`); integritasnya dijaga service
> integrasi. Tidak ada FK ke database SIMRS.
