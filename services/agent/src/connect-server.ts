import { connectNodeAdapter } from "@connectrpc/connect-node";
import { createServer as createHttpServer } from "http";
import { agentServiceImpl } from "./grpc-service.js";
import { AgentService } from "../gen/netclode/v1/agent_pb.js";

const agentPort = parseInt(process.env.AGENT_PORT || "3002", 10);

export function startConnectServer() {
  // connectNodeAdapter takes routes as a function that registers services
  const handler = connectNodeAdapter({
    routes: (router) => {
      router.service(AgentService, agentServiceImpl);
    },
  });

  const server = createHttpServer((req, res) => {
    // Health check endpoint
    if (req.url === "/health" && req.method === "GET") {
      res.writeHead(200);
      res.end("ok");
      return;
    }

    // Connect handler
    handler(req, res);
  });

  server.listen(agentPort, () => {
    console.log(`[agent] Connect server listening on http://localhost:${agentPort}`);
  });

  return server;
}
