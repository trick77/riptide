from httpx import AsyncClient


class TestHealth:
    async def test_health_returns_200(self, client: AsyncClient) -> None:
        response = await client.get("/health")
        assert response.status_code == 200
        assert response.json() == {"status": "ok"}

    async def test_ready_with_db_ok(self, client: AsyncClient) -> None:
        response = await client.get("/ready")
        assert response.status_code == 200
        body = response.json()
        assert body["status"] == "ok"
        assert body["teams"] >= 1
        assert body["team_keys"] >= 1
