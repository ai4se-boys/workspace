import os
import json
import re
from typing import List, Dict, Optional

data = json.load(open("after_dataset.json"))


def extract_first_code_block(text: str) -> Optional[str]:
    pattern = re.compile(r"```python(.*?)```", re.DOTALL)

    match = pattern.search(text)

    if match:
        return match.group(1).strip()

    return None

for item in data:
    function_statement = extract_first_code_block(item['function_statement'])
    if function_statement:
        item['function_statement'] = function_statement

json.dump(data, open("after1_dataset.json", "w"), indent=2)
