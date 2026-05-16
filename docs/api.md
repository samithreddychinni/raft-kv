# the rest api: cluster interface

The REST API is the external interface to the RaftKV cluster. I kept it minimal so the focus remains on the consensus protocol and durability logic.

---

## endpoints

*   **`GET /`**: Dumps the entire in-memory state as JSON.
*   **`GET /key/{name}`**: Retrieves a specific value.
*   **`POST /key/{name}`**: Proposes a state change via the Raft cluster. Expects JSON: `{"value": "..."}`.
*   **`DELETE /key/{name}`**: Proposes a deletion.

---

## redirection (the leader-only rule)

In Raft, only the **Leader** can accept writes. If you try to `POST` to a follower, they will redirect you.

*   **Status**: `503 Service Unavailable`
*   **Header**: `X-Raft-Leader: <address>`

The client should parse this header and retry the request against the leader's address. This ensures linearizability for all writes.

---

## latency & linearizability

Write requests block until the command is replicated to a majority and flushed to the disk. A `201 Created` response is a guarantee that the data is persistent and consistent across the cluster.
