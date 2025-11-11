"""
LLM工厂：创建不同类型的语言模型实例
"""

from typing import Optional, Dict, Any


def create_llm(config: Dict[str, Any]):
    """
    根据配置创建LLM实例
    
    Args:
        config: LLM配置字典
        
    Returns:
        LLM实例或None
    """
    if not config.get("enabled", False):
        return None
    
    provider = config.get("provider", "openai").lower()
    
    try:
        if provider == "openai":
            return _create_openai_llm(config)
        elif provider == "local" or provider == "ollama":
            return _create_local_llm(config)
        elif provider == "anthropic":
            return _create_anthropic_llm(config)
        else:
            print(f"不支持的LLM提供商: {provider}")
            return None
    except Exception as e:
        print(f"创建LLM失败: {str(e)}")
        print("提示: 请检查配置和依赖包是否正确安装")
        return None


def _create_openai_llm(config: Dict[str, Any]):
    """创建OpenAI LLM"""
    try:
        from langchain_openai import ChatOpenAI
        
        api_key = config.get("api_key")
        if not api_key:
            print("警告: OpenAI API密钥未配置")
            return None
        
        base_url = config.get("base_url")
        model_name = config.get("model_name", "gpt-3.5-turbo")
        
        llm = ChatOpenAI(
            model_name=model_name,
            api_key=api_key,
            base_url=base_url,
            temperature=config.get("temperature", 0.7),
            max_tokens=config.get("max_tokens", 1000)
        )
        print(f"已创建OpenAI LLM: {model_name}")
        return llm
    except ImportError:
        print("错误: 需要安装 langchain-openai")
        print("安装命令: pip install langchain-openai")
        return None


def _create_local_llm(config: Dict[str, Any]):
    """创建本地LLM（Ollama等）"""
    try:
        from langchain_community.llms import Ollama
        from langchain_openai import ChatOpenAI
        
        base_url = config.get("base_url", "http://localhost:11434")
        model_name = config.get("model_name", "llama2")
        
        # 尝试使用Ollama
        try:
            llm = Ollama(
                model=model_name,
                base_url=base_url,
                temperature=config.get("temperature", 0.7)
            )
            print(f"已创建Ollama LLM: {model_name}")
            return llm
        except:
            # 如果Ollama不可用，尝试使用OpenAI兼容的本地API
            llm = ChatOpenAI(
                model_name=model_name,
                base_url=base_url,
                api_key="not-needed",  # 本地API可能不需要key
                temperature=config.get("temperature", 0.7),
                max_tokens=config.get("max_tokens", 1000)
            )
            print(f"已创建本地LLM: {model_name} (通过 {base_url})")
            return llm
    except ImportError:
        print("错误: 需要安装 langchain-community")
        print("安装命令: pip install langchain-community")
        return None


def _create_anthropic_llm(config: Dict[str, Any]):
    """创建Anthropic LLM"""
    try:
        from langchain_anthropic import ChatAnthropic
        
        api_key = config.get("api_key")
        if not api_key:
            print("警告: Anthropic API密钥未配置")
            return None
        
        model_name = config.get("model_name", "claude-3-sonnet-20240229")
        
        llm = ChatAnthropic(
            model=model_name,
            api_key=api_key,
            temperature=config.get("temperature", 0.7),
            max_tokens=config.get("max_tokens", 1000)
        )
        print(f"已创建Anthropic LLM: {model_name}")
        return llm
    except ImportError:
        print("错误: 需要安装 langchain-anthropic")
        print("安装命令: pip install langchain-anthropic")
        return None

