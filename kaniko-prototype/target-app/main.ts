import { serve } from "https://deno.land/std@0.177.0/http/server.ts";

const port = 8080;

const handler = (request: Request): Response => {
  return new Response("Hello from dynamically built Deno image via Nixpacks + Kaniko!\n", { status: 200 });
};

console.log(`Listening on http://localhost:${port}/`);
serve(handler, { port });
