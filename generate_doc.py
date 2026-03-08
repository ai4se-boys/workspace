from openai import AsyncAzureOpenAI
from typing import Optional, Dict, Any, final
from dataclasses import dataclass
import json
import asyncio

@final
@dataclass
class AzureOpenAIConfig:
    """Configuration for Azure OpenAI API."""
    api_key: str
    azure_endpoint: str
    api_version: str
    model: str
    max_retries: int = 3
    timeout: int = 60


qwen3_config = AzureOpenAIConfig(
    api_key="MP3SzQbssbgDeerOfC3DLipCBVur1Qm9_GPT_AK",
    azure_endpoint="https://search.bytedance.net/gpt/openapi/online/v2/crawl/openai/deployments/gpt_openapi",
    api_version="2024-03-01-preview",
    model="gemini-3-pro-preview"
)

qwen3_client = AsyncAzureOpenAI(
    api_key="MP3SzQbssbgDeerOfC3DLipCBVur1Qm9_GPT_AK",
    azure_endpoint="https://search.bytedance.net/gpt/openapi/online/v2/crawl/openai/deployments/gpt_openapi",
    api_version="2024-03-01-preview,"
)



generate_doc_format = """
Please generate a standard docstring for the following Python function, using the Google-style document string format.
docstring should contain:
1. A brief description of the function
2. Args: Parameter Description (If parameters are available)
3. Returns: Return Value Description (If there is a return value)
4. Raises: possible exceptions thrown (if applicable)

function code:
```python
{function_code}
```

    Please return only the docstring content and do not include any other explanatory text.
"""

generate_pseudocode_format = """
Please convert the following code into clear and understandable pseudocode. Requirements:

1. Use natural language to describe algorithmic logic and key steps
2. Maintain the original code's structure and control flow
3. Replace specific syntax implementations with concise statements
4. Highlight the core algorithmic concepts while minimizing language-specific details
5. Use standard pseudocode format (such as BEGIN/END, IF/THEN/ELSE, WHILE/DO, etc.)
6. Provide appropriate comments for complex operations
7. Maintain clear logical hierarchy with proper indentation

Please output in the following format:

**Algorithm Name:** [Brief description of algorithm functionality]

**Input:** [Describe input parameters]

**Output:** [Describe output results]


**Notes:** [If necessary, add time complexity, space complexity, or other important explanations]

Please convert the following code to pseudocode:
```python
{function_code}
```
"""


async def generate(client: AsyncAzureOpenAI, function_code: str, prompt_format: str) -> str:

    prompt = prompt_format.format(function_code=function_code)

    try:
        response = await client.chat.completions.create(
            model=qwen3_config.model,
            messages=[
                {
                    "role": "system",
                    "content": "你是一个专业的Python开发者，擅长编写清晰、准确的函数文档。"
                },
                {
                    "role": "user",
                    "content": prompt
                }
            ],
            temperature=0.3,
            max_tokens=1000
        )
        if response.choices[0].message.content is None:
            raise Exception("生成docstring失败: 空响应")
        docstring = response.choices[0].message.content.strip()
        return docstring
    except Exception as e:
        raise Exception(f"生成docstring失败: {str(e)}")


async def hander(item: Dict[str, Any], semaphore: asyncio.Semaphore):
    async with semaphore:
        function_code = item["ground_truth"]
        docstring = await generate(qwen3_client, function_code, generate_pseudocode_format)
        # item["function_statement"] = docstring
        item["function_pseudocode"] = docstring
        print(item)




async def process_items_concurrently(data: list, max_concurrent: int = 5):
    """并发处理所有项目，控制并发数量"""
    semaphore = asyncio.Semaphore(max_concurrent)
    # 添加进度条
    tasks = [hander(item, semaphore) for item in data]
    await asyncio.gather(*tasks)



def main():
    data = json.load(open("after1_dataset.json"))
    asyncio.run(process_items_concurrently(data))
    json.dump(data, open("after2_dataset.json", "w"), indent=2)

if __name__ == "__main__":
    main()