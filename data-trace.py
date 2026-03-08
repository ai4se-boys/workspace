import json
import pandas as pd

data = json.load(open("after_dataset.json"))
# 转换为DataFrame
df = pd.DataFrame(data)
# 对 git_repo 字段进行聚合
repo_counts = df['git_repo'].value_counts()
print(repo_counts)

# git@github.com: ai4se-boys/dapr.git          83
# git@github.com: ai4se-boys/ent.git           68
# git@github.com: ai4se-boys/fiber.git         48
# git@github.com: ai4se-boys/livekit.git       39
# https: // github.com/ai4se-boys/grpc-go       36
# git@github.com: ai4se-boys/go-swagger.git    34
# git@github.com: ai4se-boys/ollama.git        27
# git@github.com: ai4se-boys/go-micro.git      27
# git@github.com: ai4se-boys/oauth2.git        21
# git@github.com: ai4se-boys/beego.git          8
# git@github.com: ai4se-boys/gin.git            4