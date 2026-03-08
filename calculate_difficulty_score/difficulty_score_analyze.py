import pandas as pd

df = pd.read_json('dataset_handlered.json', lines=False)

# 读取 difficulty_score 列
difficulty_scores = df['difficulty_score']

# 对 difficulty_scores 进行统计分析
difficulty_scores.describe()
print(difficulty_scores.describe())
# 将数据分成两个 level 按照难度，一个难度是低于 20 的，一个难度是高于等于 20 的
df['difficulty_level'] = df['difficulty_score'].apply(lambda x: 'low' if x < 40 else 'high')
low_difficulty = df[df['difficulty_level'] == 'low']
high_difficulty = df[df['difficulty_level'] == 'high']

# 统计每个 level 的样本数量
print(low_difficulty.shape[0])
print(high_difficulty.shape[0])

# 分别对 low_difficulty 和 high_difficulty 进行统计分析
print(low_difficulty['difficulty_score'].describe())
print(high_difficulty['difficulty_score'].describe())

# 可视化
import matplotlib.pyplot as plt

plt.hist(low_difficulty['difficulty_score'], bins=20, alpha=0.5, label='Low Difficulty')
plt.hist(high_difficulty['difficulty_score'], bins=20, alpha=0.5, label='High Difficulty')
plt.legend()
plt.xlabel('Difficulty Score')
plt.ylabel('Frequency')
plt.title('Distribution of Difficulty Scores')
plt.show()

# 将处理之后的数据保存到 dataset_handlered.json 文件中
df.to_json('dataset_handlered_difficulty_level.json',
           orient='records', lines=False, force_ascii=False)

df_hard = df[df['difficulty_level'] == 'high']
df_hard.to_json('dataset_handlered_difficulty_level_hard.json',
           orient='records', lines=False, force_ascii=False)

df_easy = df[df['difficulty_level'] == 'low']
df_easy.to_json('dataset_handlered_difficulty_level_easy.json',
           orient='records', lines=False, force_ascii=False)