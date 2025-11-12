import asyncio
from typing import Optional
from contextlib import AsyncExitStack
import json

from mcp import ClientSession, StdioServerParameters
from mcp.client.stdio import stdio_client

class MCPClient:
    def __init__(self):
        # Initialize session and client objects
        self.session: Optional[ClientSession] = None
        self.exit_stack = AsyncExitStack()

    async def connect_to_server(self, server_script_path: str):
        """Connect to an MCP server
        
        Args:
            server_script_path: Path to the server script (.py or .js)
        """
        is_python = server_script_path.endswith('.py')
        is_js = server_script_path.endswith('.js')
        if not (is_python or is_js):
            raise ValueError("Server script must be a .py or .js file")
            
        command = "python" if is_python else "node"
        server_params = StdioServerParameters(
            command=command,
            args=[server_script_path],
            env=None
        )
        
        stdio_transport = await self.exit_stack.enter_async_context(stdio_client(server_params))
        self.stdio, self.write = stdio_transport
        self.session = await self.exit_stack.enter_async_context(ClientSession(self.stdio, self.write))
        
        await self.session.initialize()
        
        # List available tools
        response = await self.session.list_tools()
        tools = response.tools
        print("\nConnected to server with tools:", [tool.name for tool in tools])
        for tool in tools:
            print(f"Tool name is: {tool.name}")
            print(f"Tool description is: {tool.description}")
            print(f"Tool inputSchema is {tool.inputSchema}")
    
    async def run_tool(self, tool_name, tool_args=None):
                
        # Execute tool call
        result = await self.session.call_tool(tool_name, tool_args)

        return result.content[0].text
    
    async def cleanup(self):
        """Clean up resources"""
        await self.exit_stack.aclose()

async def main():
    if len(sys.argv) < 3:
        print("Usage: python client.py <path_to_server_script> <tool_name> (optional)<tool_args_dict>")
        sys.exit(1)
        
    client = MCPClient()
    try:
        await client.connect_to_server(sys.argv[1])
        if len(sys.argv) == 4:
            return await client.run_tool(sys.argv[2], json.loads(sys.argv[3]))
        else:
            return await client.run_tool(sys.argv[2])
        #await client.chat_loop()
    finally:
        await client.cleanup()

if __name__ == "__main__":
    import sys
    print(asyncio.run(main()), end="")